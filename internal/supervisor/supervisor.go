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
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tanzir71/dropserve/internal/app"
)

const healthDeadline = 30 * time.Second

const maximumStartAttempts = 5

const restartFailureWindow = 10 * time.Minute

// Options controls restart timing and durable log placement. Empty timing uses the production policy.
type Options struct {
	RestartDelays []time.Duration
	LogDirectory  string
	PortPath      string
}

// Snapshot is the observable state and bounded logs for one command app.
type Snapshot struct {
	Status         string `json:"status"`
	Attempts       int    `json:"attempts"`
	Logs           string `json:"logs"`
	Port           int    `json:"port"`
	PrefersOwnPort bool   `json:"prefers_own_port"`
}

// Manager preserves healthy command processes across scanner reconciliations.
type Manager struct {
	mu        sync.Mutex
	processes map[string]*Process
	options   Options
	closed    bool
	ports     *portRegistry
	portErr   error
}

// NewManager creates an empty process manager.
func NewManager(options Options) *Manager {
	if len(options.RestartDelays) == 0 {
		options.RestartDelays = []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}
	}
	ports, err := newPortRegistry(options.PortPath)
	return &Manager{processes: make(map[string]*Process), options: options, ports: ports, portErr: err}
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
		return manager.publishLocked(key, stopped, nil), nil
	}
	return manager.startLocked(application, nil, nil)
}

func (manager *Manager) startLocked(
	application app.App,
	mount *managedHandler,
	failureTimes []time.Time,
) (http.Handler, error) {
	key := filepath.Clean(application.Path)
	if manager.portErr != nil {
		return nil, manager.portErr
	}
	port, err := manager.ports.assign(application.Slug)
	if err != nil {
		return nil, fmt.Errorf("assign port for %s: %w", application.Slug, err)
	}
	if len(application.Command) != 0 {
		if _, err := exec.LookPath(application.Command[0]); err != nil {
			missing := newProcess(application)
			missing.port = port
			missing.status = "needs-runtime"
			missing.proxy = needsRuntimeHandler(application)
			return manager.publishLocked(key, missing, mount), nil
		}
	}
	logs, err := newLogSink(manager.options.LogDirectory, application.Slug)
	if err != nil {
		return nil, fmt.Errorf("prepare logs for %s: %w", application.Slug, err)
	}
	var lastError error
	attemptLimit := maximumStartAttempts - len(failureTimes)
	for attempt := 1; attempt <= attemptLimit; attempt++ {
		process := newProcess(application)
		process.port = port
		process.logs = logs.ring
		process.output = logs
		process.attempts = len(failureTimes) + attempt
		process.failureTimes = append([]time.Time(nil), failureTimes...)
		err = process.Start()
		if err == nil {
			process.status = "ready"
			process.logCloser = logs
			handler := manager.publishLocked(key, process, mount)
			go manager.monitor(process)
			return handler, nil
		}
		lastError = err
		if attempt < attemptLimit {
			delayIndex := attempt - 1
			if delayIndex >= len(manager.options.RestartDelays) {
				delayIndex = len(manager.options.RestartDelays) - 1
			}
			timer := time.NewTimer(manager.options.RestartDelays[delayIndex])
			<-timer.C
		}
	}
	crashed := newProcess(application)
	crashed.port = port
	crashed.logs = logs.ring
	crashed.output = logs
	crashed.logCloser = logs
	crashed.attempts = len(failureTimes) + attemptLimit
	crashed.failureTimes = append([]time.Time(nil), failureTimes...)
	crashed.status = "crashed"
	crashed.proxy = crashedHandler(application, lastError)
	return manager.publishLocked(key, crashed, mount), nil
}

func (manager *Manager) startLazy(application app.App, placeholder *Process) (http.Handler, error) {
	key := filepath.Clean(application.Path)
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if current, found := manager.processes[key]; found && current != placeholder {
		return current.Handler(), nil
	}
	delete(manager.processes, key)
	handler, err := manager.startLocked(application, placeholder.mount, nil)
	if err != nil {
		manager.processes[key] = placeholder
		placeholder.mount.Set(placeholder.proxy)
	}
	return handler, err
}

func (manager *Manager) publishLocked(key string, process *Process, mount *managedHandler) http.Handler {
	if mount == nil {
		mount = newManagedHandler(process.proxy)
	} else {
		mount.Set(process.proxy)
	}
	process.mount = mount
	manager.processes[key] = process
	return mount
}

func (manager *Manager) monitor(process *Process) {
	exitError, received := <-process.done
	if !received {
		return
	}
	key := filepath.Clean(process.application.Path)
	now := time.Now()
	manager.mu.Lock()
	if manager.closed || manager.processes[key] != process {
		manager.mu.Unlock()
		return
	}
	failureTimes := process.failureTimes[:0]
	for _, failedAt := range process.failureTimes {
		if now.Sub(failedAt) <= restartFailureWindow {
			failureTimes = append(failureTimes, failedAt)
		}
	}
	failureTimes = append(failureTimes, now)
	process.failureTimes = failureTimes
	if len(failureTimes) >= maximumStartAttempts {
		process.status = "crashed"
		process.attempts = len(failureTimes)
		process.proxy = crashedHandler(process.application, fmt.Errorf("exited after becoming healthy: %w", exitError))
		process.mount.Set(process.proxy)
		_ = process.Close()
		manager.mu.Unlock()
		return
	}
	process.status = "starting"
	process.mount.Set(startingHandler(process.application))
	delay := manager.options.RestartDelays[0]
	manager.mu.Unlock()

	timer := time.NewTimer(delay)
	<-timer.C
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.closed || manager.processes[key] != process {
		return
	}
	_ = process.Close()
	delete(manager.processes, key)
	if _, err := manager.startLocked(process.application, process.mount, failureTimes); err != nil {
		process.status = "crashed"
		process.proxy = crashedHandler(process.application, err)
		manager.publishLocked(key, process, process.mount)
	}
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

type managedHandler struct {
	mu     sync.RWMutex
	target http.Handler
}

func newManagedHandler(target http.Handler) *managedHandler {
	return &managedHandler{target: target}
}

func (handler *managedHandler) Set(target http.Handler) {
	handler.mu.Lock()
	defer handler.mu.Unlock()
	handler.target = target
}

func (handler *managedHandler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	handler.mu.RLock()
	target := handler.target
	handler.mu.RUnlock()
	if target == nil {
		http.Error(response, "Dropserve is preparing this app.", http.StatusServiceUnavailable)
		return
	}
	target.ServeHTTP(response, request)
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
	manager.closed = true
	var stopErrors []error
	for key, process := range manager.processes {
		if err := process.Close(); err != nil {
			stopErrors = append(stopErrors, err)
		}
		delete(manager.processes, key)
	}
	if manager.ports != nil {
		manager.ports.release()
	}
	return errors.Join(stopErrors...)
}

// Process is one healthy command app and its reverse proxy.
type Process struct {
	application    app.App
	command        *exec.Cmd
	port           int
	proxy          http.Handler
	done           chan error
	logs           *ringBuffer
	output         io.Writer
	logCloser      io.Closer
	closeOnce      sync.Once
	closeErr       error
	control        *processControl
	mount          *managedHandler
	status         string
	attempts       int
	failureTimes   []time.Time
	prefersOwnPort bool
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
	port := process.port
	if port == 0 {
		var err error
		port, err = availablePort()
		if err != nil {
			return err
		}
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
	director := proxy.Director
	proxy.Director = func(request *http.Request) {
		publicHost := request.Host
		publicProtocol := "http"
		if request.TLS != nil {
			publicProtocol = "https"
		}
		director(request)
		prefix := "/" + process.application.Slug
		request.Header.Set("X-Forwarded-Prefix", prefix)
		request.Header.Set("X-Script-Name", prefix)
		request.Header.Set("X-Forwarded-Host", publicHost)
		request.Header.Set("X-Forwarded-Proto", publicProtocol)
	}
	proxy.ModifyResponse = func(response *http.Response) error {
		prefix := "/" + process.application.Slug
		if cookies := response.Header.Values("Set-Cookie"); len(cookies) != 0 {
			response.Header.Del("Set-Cookie")
			for _, cookie := range cookies {
				response.Header.Add("Set-Cookie", rewriteRootCookiePath(cookie, prefix))
			}
		}
		if response.StatusCode >= http.StatusMultipleChoices && response.StatusCode < http.StatusBadRequest {
			location := response.Header.Get("Location")
			if strings.HasPrefix(location, "/") &&
				!strings.HasPrefix(location, "//") &&
				location != prefix &&
				!strings.HasPrefix(location, prefix+"/") {
				response.Header.Set("Location", prefix+location)
			}
		}
		return rewriteHTMLResponse(response, prefix, process.application.BaseHref)
	}
	proxy.ErrorHandler = func(response http.ResponseWriter, _ *http.Request, proxyErr error) {
		http.Error(response, "Dropserve could not reach this app: "+proxyErr.Error(), http.StatusBadGateway)
	}
	process.proxy = proxy
	process.prefersOwnPort = probeRootAbsoluteReferences(target.String())
	return nil
}

// Handler returns the healthy loopback reverse proxy.
func (process *Process) Handler() http.Handler {
	if process.mount != nil {
		return process.mount
	}
	return process.proxy
}

// Snapshot returns this process's current state and bounded logs.
func (process *Process) Snapshot() Snapshot {
	return Snapshot{
		Status:         process.status,
		Attempts:       process.attempts,
		Logs:           process.logs.String(),
		Port:           process.port,
		PrefersOwnPort: process.prefersOwnPort,
	}
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
		var err error
		if process.control != nil {
			err = process.control.stop(process.command)
		} else {
			err = process.command.Process.Kill()
		}
		if err != nil && !errors.Is(err, os.ErrProcessDone) {
			process.closeErr = err
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
	basePath := "/" + application.Slug + "/"
	overrides := map[string]string{
		strings.ToUpper(portVariable): strconv.Itoa(port),
		"HOST":                        "127.0.0.1",
		"DROPSERVE_BASE_PATH":         basePath,
		"DROPSERVE_BASE_URL":          "http://127.0.0.1" + basePath,
	}
	defaults := map[string]string{
		"BASE_PATH":             basePath,
		"PUBLIC_URL":            basePath,
		"VITE_BASE":             basePath,
		"NEXT_PUBLIC_BASE_PATH": basePath,
	}
	type environmentValue struct {
		name  string
		value string
	}
	appEnvironment := make(map[string]environmentValue, len(application.Environment))
	for name, value := range application.Environment {
		appEnvironment[environmentNameKey(name)] = environmentValue{name: name, value: value}
	}
	present := make(map[string]struct{}, len(os.Environ())+len(appEnvironment))
	environment := make([]string, 0, len(os.Environ())+len(appEnvironment)+len(overrides)+len(defaults))
	for _, item := range os.Environ() {
		name, _, found := strings.Cut(item, "=")
		normalizedName := environmentNameKey(name)
		present[normalizedName] = struct{}{}
		_, replacedByApp := appEnvironment[normalizedName]
		_, replacedByDropserve := overrides[normalizedName]
		if found && (replacedByApp || replacedByDropserve) {
			continue
		}
		environment = append(environment, item)
	}
	for key, item := range appEnvironment {
		present[key] = struct{}{}
		if _, reserved := overrides[key]; !reserved {
			environment = append(environment, item.name+"="+item.value)
		}
	}
	for name, value := range overrides {
		environment = append(environment, name+"="+value)
	}
	for name, value := range defaults {
		if _, exists := present[name]; !exists {
			environment = append(environment, name+"="+value)
		}
	}
	return environment
}

func environmentNameKey(name string) string {
	if runtime.GOOS == "windows" {
		return strings.ToUpper(name)
	}
	return name
}

func rewriteRootCookiePath(cookie, prefix string) string {
	attributes := strings.Split(cookie, ";")
	for index, attribute := range attributes {
		trimmed := strings.TrimSpace(attribute)
		name, value, found := strings.Cut(trimmed, "=")
		if !found || !strings.EqualFold(name, "Path") || value != "/" {
			continue
		}
		leadingSpace := attribute[:len(attribute)-len(strings.TrimLeft(attribute, " \t"))]
		attributes[index] = leadingSpace + "Path=" + prefix + "/"
	}
	return strings.Join(attributes, ";")
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

func startingHandler(application app.App) http.Handler {
	name := html.EscapeString(application.Name)
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		response.Header().Set("Retry-After", "1")
		response.WriteHeader(http.StatusOK)
		if request.Method == http.MethodHead {
			return
		}
		_, _ = fmt.Fprintf(
			response,
			"<!doctype html><meta http-equiv=\"refresh\" content=\"1\"><title>%s is starting · Dropserve</title><h1>%s is starting…</h1><p>This page will try again in a moment.</p>",
			name,
			name,
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
