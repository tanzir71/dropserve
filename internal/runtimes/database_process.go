package runtimes

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const databaseStartTimeout = 20 * time.Second

type childDatabase struct {
	mu         sync.Mutex
	command    *exec.Cmd
	done       chan error
	connection string
	closed     bool
}

func startDatabaseRuntime(ctx context.Context, pack Pack, executable, dataDirectory string, output io.Writer) (DatabaseRuntime, error) {
	if pack.Name != "mariadb" && pack.Name != "postgres" {
		return nil, fmt.Errorf("unsupported database add-on %q", pack.Name)
	}
	if err := initializeDatabase(ctx, pack.Name, executable, dataDirectory, output); err != nil {
		return nil, err
	}
	port, err := availableDatabasePort(ctx)
	if err != nil {
		return nil, fmt.Errorf("reserve %s port: %w", pack.Name, err)
	}
	arguments := databaseArguments(pack.Name, executable, dataDirectory, port)
	command := exec.CommandContext(ctx, executable, arguments...) // #nosec G204 -- executable is from a verified pinned pack.
	command.Stdout = output
	command.Stderr = output
	configureRuntimeCommand(command)
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", addonTitle(pack.Name), err)
	}
	process := &childDatabase{command: command, done: make(chan error, 1)}
	if pack.Name == "mariadb" {
		process.connection = fmt.Sprintf("mysql://root@127.0.0.1:%d/", port)
	} else {
		process.connection = fmt.Sprintf("postgresql://postgres@127.0.0.1:%d/postgres", port)
	}
	go func() {
		process.done <- command.Wait()
		close(process.done)
	}()
	if err := waitForDatabase(ctx, process, port, databaseStartTimeout); err != nil {
		_ = process.Close()
		return nil, fmt.Errorf("start %s: %w", addonTitle(pack.Name), err)
	}
	return process, nil
}

func initializeDatabase(ctx context.Context, name, executable, dataDirectory string, output io.Writer) error {
	marker := filepath.Join(dataDirectory, ".dropserve-initialized")
	if _, err := os.Stat(marker); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect %s data: %w", addonTitle(name), err)
	}
	var initializer string
	var arguments []string
	switch name {
	case "mariadb":
		initializer = filepath.Join(filepath.Dir(executable), "mariadb-install-db.exe")
		arguments = []string{"--datadir=" + dataDirectory, "--password="}
	case "postgres":
		initializer = filepath.Join(filepath.Dir(executable), "initdb.exe")
		arguments = []string{"-D", dataDirectory, "-U", "postgres", "--auth=trust", "--encoding=UTF8", "--no-locale"}
	default:
		return fmt.Errorf("unsupported database add-on %q", name)
	}
	if _, err := os.Stat(initializer); err != nil {
		return fmt.Errorf("find %s initializer: %w", addonTitle(name), err)
	}
	command := exec.CommandContext(ctx, initializer, arguments...) // #nosec G204 -- initializer is adjacent to a verified pinned executable.
	command.Stdout = output
	command.Stderr = output
	configureRuntimeCommand(command)
	if err := command.Run(); err != nil {
		return fmt.Errorf("initialize %s data: %w", addonTitle(name), err)
	}
	if err := os.WriteFile(marker, []byte("managed by Dropserve\n"), 0o600); err != nil {
		return fmt.Errorf("record %s initialization: %w", addonTitle(name), err)
	}
	return nil
}

func databaseArguments(name, executable, dataDirectory string, port int) []string {
	switch name {
	case "mariadb":
		baseDirectory := filepath.Dir(filepath.Dir(executable))
		return []string{
			"--basedir=" + baseDirectory,
			"--datadir=" + dataDirectory,
			"--bind-address=127.0.0.1",
			"--port=" + strconv.Itoa(port),
			"--skip-name-resolve",
			"--console",
		}
	case "postgres":
		return []string{"-D", dataDirectory, "-h", "127.0.0.1", "-p", strconv.Itoa(port)}
	default:
		return nil
	}
}

func availableDatabasePort(ctx context.Context) (int, error) {
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		return 0, err
	}
	return port, nil
}

func waitForDatabase(ctx context.Context, process *childDatabase, port int, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	for {
		connection, err := (&net.Dialer{Timeout: 150 * time.Millisecond}).DialContext(ctx, "tcp", address)
		if err == nil {
			_ = connection.Close()
			return nil
		}
		select {
		case processErr := <-process.done:
			return fmt.Errorf("process exited before accepting connections: %w", processErr)
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("did not listen on %s within %s", address, timeout)
		case <-ticker.C:
		}
	}
}

func (process *childDatabase) Running() bool {
	process.mu.Lock()
	defer process.mu.Unlock()
	if process.closed {
		return false
	}
	select {
	case <-process.done:
		return false
	default:
		return true
	}
}

func (process *childDatabase) Connection() string { return process.connection }

func (process *childDatabase) Close() error {
	process.mu.Lock()
	if process.closed {
		process.mu.Unlock()
		return nil
	}
	process.closed = true
	command := process.command
	process.mu.Unlock()
	select {
	case <-process.done:
		return nil
	default:
	}
	if command.Process != nil {
		if err := command.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return fmt.Errorf("stop database process: %w", err)
		}
	}
	select {
	case <-process.done:
		return nil
	case <-time.After(5 * time.Second):
		return errors.New("database process did not exit within 5s")
	}
}
