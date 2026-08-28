package runtimes

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	phpfastcgi "github.com/tanzir71/dropserve/internal/php"
)

// PHPRuntime is the app-facing portion of a supervised php-cgi pool.
type PHPRuntime interface {
	Handler(documentRoot, slug string) http.Handler
	Close() error
}

// DatabaseRuntime is one supervised local database process.
type DatabaseRuntime interface {
	Running() bool
	Connection() string
	Close() error
}

// AddonStatus is the current dashboard-safe state of one optional pack.
type AddonStatus struct {
	Name        string
	Title       string
	Version     string
	Description string
	Available   bool
	Installed   bool
	Running     bool
	Busy        bool
	Progress    int
	Connection  string
	Message     string
}

// ManagerOptions configures optional runtimes below Dropserve's state directory.
type ManagerOptions struct {
	Context         context.Context
	StateDirectory  string
	Packs           []Pack
	Client          *http.Client
	Output          io.Writer
	OnChange        func()
	PHPStarter      func(context.Context, string, string, io.Writer) (PHPRuntime, error)
	DatabaseStarter func(context.Context, Pack, string, string, io.Writer) (DatabaseRuntime, error)
}

// Manager installs optional packs and owns their supervised processes.
type Manager struct {
	mu              sync.Mutex
	ctx             context.Context
	cancel          context.CancelFunc
	stateDirectory  string
	runtimeRoot     string
	packs           map[string]Pack
	order           []string
	client          *http.Client
	output          io.Writer
	onChange        func()
	phpStarter      func(context.Context, string, string, io.Writer) (PHPRuntime, error)
	databaseStarter func(context.Context, Pack, string, string, io.Writer) (DatabaseRuntime, error)
	php             PHPRuntime
	databases       map[string]DatabaseRuntime
	busy            map[string]bool
	progress        map[string]int
	messages        map[string]string
	closed          bool
}

// NewManager discovers already installed packs and starts an installed PHP pack.
func NewManager(options ManagerOptions) (*Manager, error) {
	if options.StateDirectory == "" {
		return nil, errors.New("create add-on manager: state directory is required")
	}
	root, err := filepath.Abs(options.StateDirectory)
	if err != nil {
		return nil, fmt.Errorf("create add-on manager: resolve state directory: %w", err)
	}
	parent := options.Context
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	packs := options.Packs
	if packs == nil {
		packs = CurrentAddonPacks()
	}
	manager := &Manager{
		ctx: ctx, cancel: cancel, stateDirectory: root, runtimeRoot: filepath.Join(root, "runtimes"),
		packs: make(map[string]Pack, len(packs)), client: options.Client, output: options.Output,
		onChange: options.OnChange, phpStarter: options.PHPStarter, databaseStarter: options.DatabaseStarter,
		databases: make(map[string]DatabaseRuntime), busy: make(map[string]bool),
		progress: make(map[string]int), messages: make(map[string]string),
	}
	if manager.output == nil {
		manager.output = io.Discard
	}
	if manager.phpStarter == nil {
		manager.phpStarter = startPHPRuntime
	}
	if manager.databaseStarter == nil {
		manager.databaseStarter = startDatabaseRuntime
	}
	for _, pack := range packs {
		if err := validatePack(pack); err != nil {
			cancel()
			return nil, fmt.Errorf("create add-on manager: %w", err)
		}
		if _, found := manager.packs[pack.Name]; found {
			cancel()
			return nil, fmt.Errorf("create add-on manager: duplicate pack %q", pack.Name)
		}
		manager.packs[pack.Name] = pack
		manager.order = append(manager.order, pack.Name)
	}
	if pack, found := manager.packs["php"]; found {
		_, installed, inspectErr := InstalledExecutable(manager.runtimeRoot, pack)
		switch {
		case inspectErr != nil:
			manager.messages[pack.Name] = inspectErr.Error()
		case installed:
			if startErr := manager.start(pack); startErr != nil {
				manager.messages[pack.Name] = "Dropserve could not start PHP: " + startErr.Error()
			}
		}
	}
	return manager, nil
}

// Statuses returns stable manifest order for the Add-ons dashboard.
func (manager *Manager) Statuses() []AddonStatus {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	statuses := make([]AddonStatus, 0, len(manager.order))
	for _, name := range manager.order {
		pack := manager.packs[name]
		_, installed, err := InstalledExecutable(manager.runtimeRoot, pack)
		status := AddonStatus{
			Name: name, Title: addonTitle(name), Version: pack.Version, Description: addonDescription(name),
			Available: true, Installed: installed, Busy: manager.busy[name], Progress: manager.progress[name],
			Message: manager.messages[name],
		}
		if err != nil {
			status.Message = err.Error()
		}
		if name == "php" {
			status.Running = manager.php != nil
		} else if process := manager.databases[name]; process != nil {
			status.Running = process.Running()
			if status.Running {
				status.Connection = process.Connection()
			}
		}
		statuses = append(statuses, status)
	}
	return statuses
}

// Change performs one explicit install, remove, start, or stop action.
func (manager *Manager) Change(ctx context.Context, name, action string) (changeErr error) {
	manager.mu.Lock()
	pack, found := manager.packs[name]
	if !found {
		manager.mu.Unlock()
		return fmt.Errorf("unknown add-on %q", name)
	}
	if manager.closed {
		manager.mu.Unlock()
		return errors.New("add-on manager is closed")
	}
	if manager.busy[name] {
		manager.mu.Unlock()
		return fmt.Errorf("add-on %s is already changing", name)
	}
	manager.busy[name] = true
	manager.progress[name] = 0
	manager.messages[name] = ""
	manager.mu.Unlock()
	defer func() {
		manager.mu.Lock()
		manager.busy[name] = false
		if changeErr != nil {
			manager.messages[name] = changeErr.Error()
		}
		manager.mu.Unlock()
		manager.notify()
	}()

	switch action {
	case "install":
		installer := Installer{Root: manager.runtimeRoot, Client: manager.client, Progress: func(progress Progress) {
			percent := 0
			if progress.Total > 0 {
				percent = int(min(int64(99), progress.Downloaded*100/progress.Total))
			}
			manager.mu.Lock()
			manager.progress[name] = percent
			manager.mu.Unlock()
		}}
		if _, err := installer.Install(ctx, pack); err != nil {
			return err
		}
		manager.mu.Lock()
		manager.progress[name] = 100
		manager.mu.Unlock()
		if name == "php" {
			return manager.start(pack)
		}
		return nil
	case "remove":
		if err := manager.stop(name); err != nil {
			return err
		}
		if err := (Installer{Root: manager.runtimeRoot}).Remove(pack); err != nil {
			return err
		}
		if name == "php" {
			return manager.removeManagedState("php")
		}
		return manager.removeDatabaseData(name)
	case "start":
		return manager.start(pack)
	case "stop":
		return manager.stop(name)
	default:
		return fmt.Errorf("unknown add-on action %q", action)
	}
}

// PHPHandler returns nil when PHP is not installed or is not running.
func (manager *Manager) PHPHandler(documentRoot, slug string) (http.Handler, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.php == nil {
		return nil, nil
	}
	return manager.php.Handler(documentRoot, slug), nil
}

func (manager *Manager) start(pack Pack) error {
	executable, installed, err := InstalledExecutable(manager.runtimeRoot, pack)
	if err != nil {
		return err
	}
	if !installed {
		return fmt.Errorf("install %s before starting it", addonTitle(pack.Name))
	}
	manager.mu.Lock()
	if pack.Name == "php" && manager.php != nil {
		manager.mu.Unlock()
		return nil
	}
	if process := manager.databases[pack.Name]; process != nil && process.Running() {
		manager.mu.Unlock()
		return nil
	}
	manager.mu.Unlock()
	if pack.Name == "php" {
		iniPath := filepath.Join(manager.stateDirectory, "php", "php.ini")
		if err := phpfastcgi.WriteINI(iniPath, time.Now().Location().String()); err != nil {
			return err
		}
		pool, err := manager.phpStarter(manager.ctx, executable, iniPath, manager.output)
		if err != nil {
			return err
		}
		manager.mu.Lock()
		manager.php = pool
		manager.mu.Unlock()
		return nil
	}
	dataDirectory, err := DatabaseDataDirectory(manager.stateDirectory, pack.Name)
	if err != nil {
		return err
	}
	process, err := manager.databaseStarter(manager.ctx, pack, executable, dataDirectory, manager.output)
	if err != nil {
		return err
	}
	manager.mu.Lock()
	manager.databases[pack.Name] = process
	manager.mu.Unlock()
	return nil
}

func (manager *Manager) stop(name string) error {
	manager.mu.Lock()
	if name == "php" {
		pool := manager.php
		manager.php = nil
		manager.mu.Unlock()
		if pool != nil {
			return pool.Close()
		}
		return nil
	}
	process := manager.databases[name]
	delete(manager.databases, name)
	manager.mu.Unlock()
	if process != nil {
		return process.Close()
	}
	return nil
}

func (manager *Manager) removeDatabaseData(name string) error {
	if name != "mariadb" && name != "postgres" {
		return fmt.Errorf("refuse to remove data for unknown database engine %q", name)
	}
	dataRoot := filepath.Join(manager.stateDirectory, "databases")
	target := filepath.Join(dataRoot, name)
	relative, err := filepath.Rel(dataRoot, target)
	if err != nil || relative == "." || filepath.IsAbs(relative) || relative == ".." {
		return fmt.Errorf("refuse to remove database data outside the state directory")
	}
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("remove %s data: %w", name, err)
	}
	return nil
}

func (manager *Manager) removeManagedState(name string) error {
	if name != "php" {
		return fmt.Errorf("refuse to remove managed state for unknown add-on %q", name)
	}
	target := filepath.Join(manager.stateDirectory, name)
	relative, err := filepath.Rel(manager.stateDirectory, target)
	if err != nil || relative != name {
		return fmt.Errorf("refuse to remove add-on state outside the state directory")
	}
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("remove %s state: %w", name, err)
	}
	return nil
}

func (manager *Manager) notify() {
	if manager.onChange != nil {
		manager.onChange()
	}
}

// Close stops all optional child processes.
func (manager *Manager) Close() error {
	manager.mu.Lock()
	if manager.closed {
		manager.mu.Unlock()
		return nil
	}
	manager.closed = true
	phpPool := manager.php
	manager.php = nil
	processes := make([]DatabaseRuntime, 0, len(manager.databases))
	for _, process := range manager.databases {
		processes = append(processes, process)
	}
	manager.databases = make(map[string]DatabaseRuntime)
	manager.mu.Unlock()
	manager.cancel()
	var closeErrors []error
	if phpPool != nil {
		closeErrors = append(closeErrors, phpPool.Close())
	}
	for _, process := range processes {
		closeErrors = append(closeErrors, process.Close())
	}
	return errors.Join(closeErrors...)
}

func startPHPRuntime(ctx context.Context, executable, iniPath string, output io.Writer) (PHPRuntime, error) {
	return phpfastcgi.StartPool(ctx, phpfastcgi.PoolOptions{Executable: executable, INIPath: iniPath, Output: output})
}

func addonTitle(name string) string {
	switch name {
	case "php":
		return "PHP"
	case "mariadb":
		return "MariaDB"
	case "postgres":
		return "PostgreSQL"
	default:
		return name
	}
}

func addonDescription(name string) string {
	switch name {
	case "php":
		return "Adds the PHP files Dropserve needs to run PHP apps."
	case "mariadb":
		return "Optional local MariaDB server with data stored by Dropserve."
	case "postgres":
		return "Optional local PostgreSQL server with data stored by Dropserve."
	default:
		return "Optional files Dropserve needs to run this kind of app."
	}
}
