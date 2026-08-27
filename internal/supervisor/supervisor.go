// Package supervisor runs command apps as loopback HTTP services.
package supervisor

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tanzir71/dropserve/internal/app"
)

const healthDeadline = 30 * time.Second

const maximumStartAttempts = 5

// Options controls restart timing and durable log placement. Empty timing uses the production policy.
type Options struct {
	RestartDelays []time.Duration
	LogDirectory  string
}

// Snapshot is the observable state and bounded logs for one command app.
type Snapshot struct {
	Status   string `json:"status"`
	Attempts int    `json:"attempts"`
	Logs     string `json:"logs"`
}

// Manager preserves healthy command processes across scanner reconciliations.
type Manager struct {
	mu        sync.Mutex
	processes map[string]*Process
	options   Options
}

// NewManager creates an empty process manager.
func NewManager(options Options) *Manager {
	if len(options.RestartDelays) == 0 {
		options.RestartDelays = []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}
	}
	return &Manager{processes: make(map[string]*Process), options: options}
}

// Handler returns a process proxy, preserving lazy apps in a stopped state until requested.
func (manager *Manager) Handler(application app.App) (http.Handler, error) {
	key := filepath.Clean(application.Path)
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if process, found := manager.processes[key]; found {
		return process.Handler(), nil
	}
	if !application.Autostart {
		stopped := newProcess(application)
		stopped.status = "stopped"
		lazy := &lazyHandler{manager: manager, application: application, placeholder: stopped}
		stopped.proxy = lazy
		manager.processes[key] = stopped
		return stopped.Handler(), nil
	}
	return manager.startLocked(application)
}

func (manager *Manager) startLocked(application app.App) (http.Handler, error) {
	key := filepath.Clean(application.Path)
	if len(application.Command) != 0 {
		if _, err := exec.LookPath(application.Command[0]); err != nil {
			missing := newProcess(application)
			missing.status = "needs-runtime"
			missing.proxy = needsRuntimeHandler(application)
			manager.processes[key] = missing
			return missing.Handler(), nil
		}
	}
	logs, err := newLogSink(manager.options.LogDirectory, application.Slug)
	if err != nil {
		return nil, fmt.Errorf("prepare logs for %s: %w", application.Slug, err)
	}
	var lastError error
	for attempt := 1; attempt <= maximumStartAttempts; attempt++ {
		process := newProcess(application)
		process.logs = logs.ring
		process.output = logs
		process.attempts = attempt
		err = process.Start()
		if err == nil {
			process.status = "ready"
			process.logCloser = logs
			manager.processes[key] = process
			return process.Handler(), nil
		}
		lastError = err
		if attempt < maximumStartAttempts {
			delayIndex := attempt - 1
			if delayIndex >= len(manager.options.RestartDelays) {
				delayIndex = len(manager.options.RestartDelays) - 1
			}
			timer := time.NewTimer(manager.options.RestartDelays[delayIndex])
			<-timer.C
		}
	}
	crashed := newProcess(application)
	crashed.logs = logs.ring
	crashed.output = logs
	crashed.logCloser = logs
	crashed.attempts = maximumStartAttempts
	crashed.status = "crashed"
	crashed.proxy = crashedHandler(application, lastError)
	manager.processes[key] = crashed
	return crashed.Handler(), nil
}

func (manager *Manager) startLazy(application app.App, placeholder *Process) (http.Handler, error) {
	key := filepath.Clean(application.Path)
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if current, found := manager.processes[key]; found && current != placeholder {
		return current.Handler(), nil
	}
	delete(manager.processes, key)
	handler, err := manager.startLocked(application)
	if err != nil {
		manager.processes[key] = placeholder
	}
	return handler, err
}

type lazyHandler struct {
	manager     *Manager
	application app.App
	placeholder *Process
}

func (handler *lazyHandler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	proxy, err := handler.manager.startLazy(handler.application, handler.placeholder)
	if err != nil {
		http.Error(response, "Dropserve could not start this app: "+err.Error(), http.StatusInternalServerError)
		return
	}
	proxy.ServeHTTP(response, request)
}

// Snapshot returns one command app's current state and bounded logs.
func (manager *Manager) Snapshot(slug string) (Snapshot, bool) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	for _, process := range manager.processes {
		if process.application.Slug == slug {
			return process.Snapshot(), true
		}
	}
	return Snapshot{}, false
}

// Reconcile stops command processes that no longer appear in the scan.
func (manager *Manager) Reconcile(applications []app.App) error {
	active := make(map[string]struct{}, len(applications))
	for _, application := range applications {
		if application.Kind == app.KindCommand {
			active[filepath.Clean(application.Path)] = struct{}{}
		}
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	var stopErrors []error
	for key, process := range manager.processes {
		if _, found := active[key]; found {
			continue
		}
		if err := process.Close(); err != nil {
			stopErrors = append(stopErrors, err)
		}
		delete(manager.processes, key)
	}
	return errors.Join(stopErrors...)
}

// Close stops every managed command process.
func (manager *Manager) Close() error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	var stopErrors []error
	for key, process := range manager.processes {
		if err := process.Close(); err != nil {
			stopErrors = append(stopErrors, err)
		}
		delete(manager.processes, key)
	}
	return errors.Join(stopErrors...)
}

// Process is one healthy command app and its reverse proxy.
type Process struct {
	application app.App
	command     *exec.Cmd
	port        int
	proxy       http.Handler
	done        chan error
	logs        *ringBuffer
	output      io.Writer
	logCloser   io.Closer
	closeOnce   sync.Once
	closeErr    error
	control     *processControl
	status      string
	attempts    int
}

func newProcess(application app.App) *Process {
	logs := newRingBuffer(memoryLogBytes)
	return &Process{application: application, logs: logs, output: logs}
}

// Start launches the command and waits until its loopback HTTP endpoint is healthy.
func (process *Process) Start() error {
	if len(process.application.Command) == 0 {
		return errors.New("command app has no command")
	}
	port, err := availablePort()
	if err != nil {
		return err
	}
	process.port = port
	control, err := newProcessControl()
	if err != nil {
		return fmt.Errorf("prepare process isolation for %s: %w", process.application.Slug, err)
	}
	command := exec.CommandContext(context.Background(), process.application.Command[0], process.application.Command[1:]...) // #nosec G204 -- command comes from local app detection/configuration.
	control.configure(command)
	command.Dir = process.application.Path
	command.Env = commandEnvironment(process.application, port)
	command.Stdout = process.output
	command.Stderr = process.output
	if err := command.Start(); err != nil {
		_ = control.close()
		return fmt.Errorf("start %s: %w", process.application.Slug, err)
	}
	process.command = command
	process.control = control
	if err := control.attach(command); err != nil {
		_ = command.Process.Kill()
		_ = control.close()
		return fmt.Errorf("isolate %s process tree: %w", process.application.Slug, err)
	}
	process.done = make(chan error, 1)
	go func() {
		process.done <- command.Wait()
		close(process.done)
	}()
	if err := process.waitHealthy(); err != nil {
		_ = process.Close()
		return err
	}
	target, err := url.Parse("http://127.0.0.1:" + strconv.Itoa(port))
	if err != nil {
		_ = process.Close()
		return fmt.Errorf("create proxy target: %w", err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = func(response http.ResponseWriter, _ *http.Request, proxyErr error) {
		http.Error(response, "Dropserve could not reach this app: "+proxyErr.Error(), http.StatusBadGateway)
	}
	process.proxy = proxy
	return nil
}

// Handler returns the healthy loopback reverse proxy.
func (process *Process) Handler() http.Handler {
	return process.proxy
}

// Snapshot returns this process's current state and bounded logs.
func (process *Process) Snapshot() Snapshot {
	return Snapshot{Status: process.status, Attempts: process.attempts, Logs: process.logs.String()}
}

// Close terminates the command and waits for it to exit.
func (process *Process) Close() error {
	process.closeOnce.Do(func() {
		defer func() {
			if process.logCloser == nil {
				return
			}
			if err := process.logCloser.Close(); process.closeErr == nil {
				process.closeErr = err
			}
		}()
		if process.command == nil || process.command.Process == nil {
			return
		}
		if process.command.ProcessState == nil || !process.command.ProcessState.Exited() {
			var err error
			if process.control != nil {
				err = process.control.stop(process.command)
			} else {
				err = process.command.Process.Kill()
			}
			if err != nil && !errors.Is(err, os.ErrProcessDone) {
				process.closeErr = err
			}
		}
		if process.done != nil {
			select {
			case <-process.done:
			case <-time.After(5 * time.Second):
				_ = process.command.Process.Kill()
				if process.closeErr == nil {
					process.closeErr = errors.New("process did not exit within five seconds")
				}
			}
		}
		if process.control != nil {
			if err := process.control.close(); process.closeErr == nil {
				process.closeErr = err
			}
		}
	})
	return process.closeErr
}

func (process *Process) waitHealthy() error {
	deadline := time.Now().Add(healthDeadline)
	delay := 100 * time.Millisecond
	client := &http.Client{Timeout: 2 * time.Second}
	healthPath := process.application.HealthPath
	if healthPath == "" {
		healthPath = "/"
	}
	target := "http://127.0.0.1:" + strconv.Itoa(process.port) + "/" + strings.TrimPrefix(healthPath, "/")
	for {
		select {
		case err := <-process.done:
			return fmt.Errorf("%s exited before it became healthy: %w; logs: %s", process.application.Slug, err, process.logs.String())
		default:
		}
		dialer := net.Dialer{Timeout: 250 * time.Millisecond}
		connection, dialErr := dialer.DialContext(
			context.Background(),
			"tcp",
			net.JoinHostPort("127.0.0.1", strconv.Itoa(process.port)),
		)
		if dialErr == nil {
			_ = connection.Close()
			request, requestErr := http.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
			if requestErr == nil {
				response, requestErr := client.Do(request)
				if requestErr == nil {
					_ = response.Body.Close()
					if response.StatusCode < http.StatusInternalServerError {
						return nil
					}
				}
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s did not become healthy within %s; logs: %s", process.application.Slug, healthDeadline, process.logs.String())
		}
		timer := time.NewTimer(delay)
		select {
		case err := <-process.done:
			if !timer.Stop() {
				<-timer.C
			}
			return fmt.Errorf("%s exited before it became healthy: %w; logs: %s", process.application.Slug, err, process.logs.String())
		case <-timer.C:
		}
		if delay < 2*time.Second {
			delay *= 2
			if delay > 2*time.Second {
				delay = 2 * time.Second
			}
		}
	}
}

func availablePort() (int, error) {
	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("allocate command-app port: %w", err)
	}
	defer func() {
		_ = listener.Close()
	}()
	_, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(portText)
}

func commandEnvironment(application app.App, port int) []string {
	portVariable := application.PortEnv
	if portVariable == "" {
		portVariable = "PORT"
	}
	overrides := map[string]string{
		strings.ToUpper(portVariable): strconv.Itoa(port),
		"HOST":                        "127.0.0.1",
		"DROPSERVE_BASE_PATH":         "/" + application.Slug + "/",
		"DROPSERVE_BASE_URL":          "http://127.0.0.1/" + application.Slug + "/",
	}
	environment := make([]string, 0, len(os.Environ())+len(overrides))
	for _, item := range os.Environ() {
		name, _, found := strings.Cut(item, "=")
		if _, replaced := overrides[strings.ToUpper(name)]; found && replaced {
			continue
		}
		environment = append(environment, item)
	}
	for name, value := range overrides {
		environment = append(environment, name+"="+value)
	}
	return environment
}

func crashedHandler(application app.App, startError error) http.Handler {
	name := html.EscapeString(application.Name)
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		response.WriteHeader(http.StatusOK)
		if request.Method == http.MethodHead {
			return
		}
		_, _ = fmt.Fprintf(
			response,
			"<!doctype html><title>%s stopped · Dropserve</title><h1>%s stopped after five starts.</h1><p>Open its logs in Dropserve to see the error.</p><!-- %s -->",
			name,
			name,
			startError,
		)
	})
}

func needsRuntimeHandler(application app.App) http.Handler {
	name := html.EscapeString(application.Name)
	runtimeName := html.EscapeString(runtimeDisplayName(application))
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		response.WriteHeader(http.StatusOK)
		if request.Method == http.MethodHead {
			return
		}
		_, _ = fmt.Fprintf(
			response,
			"<!doctype html><title>%s needs %s · Dropserve</title><h1>%s needs %s.</h1><p>To open this app, install %s and restart Dropserve.</p>",
			name,
			runtimeName,
			name,
			runtimeName,
			runtimeName,
		)
	})
}

func runtimeDisplayName(application app.App) string {
	switch strings.ToLower(application.Runtime) {
	case "node":
		return "Node.js"
	case "python":
		return "Python"
	case "ruby":
		return "Ruby"
	}
	if application.Runtime != "" {
		return application.Runtime
	}
	if len(application.Command) != 0 {
		return application.Command[0]
	}
	return "its runtime"
}

type ringBuffer struct {
	mu       sync.Mutex
	maximum  int
	contents []byte
}

func newRingBuffer(maximum int) *ringBuffer {
	return &ringBuffer{maximum: maximum, contents: make([]byte, 0, maximum)}
}

func (buffer *ringBuffer) Write(content []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	originalLength := len(content)
	if len(content) >= buffer.maximum {
		buffer.contents = append(buffer.contents[:0], content[len(content)-buffer.maximum:]...)
		return originalLength, nil
	}
	if excess := len(buffer.contents) + len(content) - buffer.maximum; excess > 0 {
		copy(buffer.contents, buffer.contents[excess:])
		buffer.contents = buffer.contents[:len(buffer.contents)-excess]
	}
	buffer.contents = append(buffer.contents, content...)
	return originalLength, nil
}

func (buffer *ringBuffer) String() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return string(append([]byte(nil), buffer.contents...))
}

func (buffer *ringBuffer) Len() int {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return len(buffer.contents)
}
