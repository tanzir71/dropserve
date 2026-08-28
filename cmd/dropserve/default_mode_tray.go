//go:build tray

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/getlantern/systray"
	"github.com/tanzir71/dropserve/internal/autostart"
	"github.com/tanzir71/dropserve/internal/dashboard"
	"github.com/tanzir71/dropserve/internal/launch"
	"github.com/tanzir71/dropserve/internal/traymenu"
)

type trayRun struct {
	cancel        context.CancelFunc
	done          chan struct{}
	code          int
	stopRequested bool
}

type trayServer struct {
	mu            sync.Mutex
	parent        context.Context
	options       defaultModeOptions
	active        *trayRun
	address       string
	lastCode      int
	trayReady     bool
	publicSharing bool
	updateNotice  dashboard.UpdateNotice
	updateItem    *systray.MenuItem
}

func runDefaultMode(ctx context.Context, options defaultModeOptions) int {
	options.ServeArguments = removeOpenFlag(options.ServeArguments)
	server := &trayServer{parent: ctx, options: options}
	if _, err := server.start(); err != nil {
		_, _ = fmt.Fprintf(options.Stderr, "Dropserve could not start for the tray: %v\n", err)
		return 1
	}

	go func() {
		<-ctx.Done()
		server.stop()
		systray.Quit()
	}()
	systray.Run(func() {
		configureTray(ctx, server)
	}, func() {
		server.stop()
	})
	return server.exitCode()
}

func removeOpenFlag(arguments []string) []string {
	result := make([]string, 0, len(arguments))
	for _, argument := range arguments {
		if argument != "--open" {
			result = append(result, argument)
		}
	}
	return result
}

func (server *trayServer) start() (string, error) {
	server.mu.Lock()
	if server.active != nil {
		address := server.address
		server.mu.Unlock()
		return address, nil
	}
	ctx, cancel := context.WithCancel(server.parent)
	run := &trayRun{cancel: cancel, done: make(chan struct{})}
	server.active = run
	server.mu.Unlock()

	ready := make(chan string, 1)
	go func() {
		run.code = serveCommandContextWithReady(
			ctx,
			server.options.ServeArguments,
			server.options.Stdout,
			server.options.Stderr,
			"",
			func(address string) { ready <- address },
			server.setPublicSharing,
			server.setUpdateNotice,
		)
		close(run.done)
	}()

	select {
	case address := <-ready:
		server.mu.Lock()
		server.address = address
		server.mu.Unlock()
		go server.watch(run)
		return address, nil
	case <-run.done:
		server.mu.Lock()
		server.lastCode = run.code
		if server.active == run {
			server.active = nil
		}
		server.mu.Unlock()
		return "", fmt.Errorf("server exited with code %d before becoming ready", run.code)
	case <-server.parent.Done():
		cancel()
		<-run.done
		return "", server.parent.Err()
	}
}

func (server *trayServer) watch(run *trayRun) {
	<-run.done
	server.mu.Lock()
	server.lastCode = run.code
	unexpected := server.active == run && !run.stopRequested
	if server.active == run {
		server.active = nil
	}
	server.mu.Unlock()
	if unexpected {
		systray.SetIcon(traymenu.Icon(traymenu.Warning))
		systray.Quit()
	}
}

func (server *trayServer) stop() {
	server.mu.Lock()
	run := server.active
	if run == nil {
		server.mu.Unlock()
		return
	}
	run.stopRequested = true
	run.cancel()
	server.mu.Unlock()
	<-run.done
	server.mu.Lock()
	if server.active == run {
		server.active = nil
	}
	server.mu.Unlock()
}

func (server *trayServer) running() bool {
	server.mu.Lock()
	defer server.mu.Unlock()
	return server.active != nil
}

func (server *trayServer) currentAddress() string {
	server.mu.Lock()
	defer server.mu.Unlock()
	return server.address
}

func (server *trayServer) exitCode() int {
	server.mu.Lock()
	defer server.mu.Unlock()
	return server.lastCode
}

func (server *trayServer) setPublicSharing(active bool) {
	server.mu.Lock()
	server.publicSharing = active
	ready := server.trayReady
	state := server.iconStateLocked()
	server.mu.Unlock()
	if ready {
		systray.SetIcon(traymenu.Icon(state))
	}
}

func (server *trayServer) setUpdateNotice(notice dashboard.UpdateNotice) {
	server.mu.Lock()
	server.updateNotice = notice
	item := server.updateItem
	server.mu.Unlock()
	refreshUpdateItem(item, notice)
}

func (server *trayServer) attachUpdateItem(item *systray.MenuItem) {
	server.mu.Lock()
	server.updateItem = item
	notice := server.updateNotice
	server.mu.Unlock()
	refreshUpdateItem(item, notice)
}

func (server *trayServer) currentUpdateURL() string {
	server.mu.Lock()
	defer server.mu.Unlock()
	return server.updateNotice.URL
}

func refreshUpdateItem(item *systray.MenuItem, notice dashboard.UpdateNotice) {
	if item == nil {
		return
	}
	if !notice.Available || notice.URL == "" {
		item.Hide()
		return
	}
	item.SetTitle(traymenu.UpdateLabel(notice.Version))
	item.Show()
}

func (server *trayServer) refreshIcon() {
	server.mu.Lock()
	ready := server.trayReady
	state := server.iconStateLocked()
	server.mu.Unlock()
	if ready {
		systray.SetIcon(traymenu.Icon(state))
	}
}

func (server *trayServer) markTrayReady() {
	server.mu.Lock()
	server.trayReady = true
	state := server.iconStateLocked()
	server.mu.Unlock()
	systray.SetIcon(traymenu.Icon(state))
}

func (server *trayServer) iconStateLocked() traymenu.State {
	if server.publicSharing {
		return traymenu.Sharing
	}
	if server.active == nil {
		return traymenu.Paused
	}
	return traymenu.Running
}

func configureTray(ctx context.Context, server *trayServer) {
	labels := traymenu.Labels()
	server.markTrayReady()
	systray.SetTitle("Dropserve")
	systray.SetTooltip("Dropserve is serving your local apps")
	openDashboard := systray.AddMenuItem(labels[0], "Open the Dropserve dashboard")
	viewUpdate := systray.AddMenuItem(traymenu.UpdateLabel(""), "Open the available Dropserve release page")
	viewUpdate.Hide()
	server.attachUpdateItem(viewUpdate)
	openApps := systray.AddMenuItem(labels[1], "Open your Apps folder")
	copyLink := systray.AddMenuItem(labels[2], "Copy this computer's Dropserve address")
	systray.AddSeparator()
	pause := systray.AddMenuItem(labels[3], "Temporarily stop serving apps")
	autostartEnabled, _ := autostart.Enabled()
	startAtLogin := systray.AddMenuItemCheckbox(labels[4], "Start Dropserve when you log in", autostartEnabled)
	systray.AddSeparator()
	runDoctor := systray.AddMenuItem(labels[5], "Open a complete support report")
	quit := systray.AddMenuItem(labels[6], "Stop Dropserve and close the tray")

	go func() {
		for {
			select {
			case <-openDashboard.ClickedCh:
				_ = launch.OpenURL(server.currentAddress())
			case <-viewUpdate.ClickedCh:
				if releaseURL := server.currentUpdateURL(); releaseURL != "" {
					_ = launch.OpenURL(releaseURL)
				}
			case <-openApps.ClickedCh:
				_ = launch.OpenPath(server.options.AppsRoot)
			case <-copyLink.ClickedCh:
				_ = copyText(ctx, server.currentAddress())
			case <-pause.ClickedCh:
				if server.running() {
					server.stop()
					server.refreshIcon()
					pause.SetTitle("Resume Serving")
					openDashboard.Disable()
					copyLink.Disable()
					continue
				}
				if _, err := server.start(); err == nil {
					server.refreshIcon()
					pause.SetTitle(labels[3])
					openDashboard.Enable()
					copyLink.Enable()
				}
			case <-startAtLogin.ClickedCh:
				if startAtLogin.Checked() {
					if err := autostart.Disable(); err == nil {
						startAtLogin.Uncheck()
					}
				} else if err := autostart.Enable(server.options.Executable); err == nil {
					startAtLogin.Check()
				}
			case <-runDoctor.ClickedCh:
				go openDoctorReport(ctx, server.options)
			case <-quit.ClickedCh:
				server.stop()
				systray.Quit()
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

func copyText(ctx context.Context, value string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		command = exec.CommandContext(ctx, "clip.exe")
	case "darwin":
		command = exec.CommandContext(ctx, "pbcopy")
	default:
		path, err := exec.LookPath("wl-copy")
		if err != nil {
			return errors.New("install wl-clipboard to copy the link from the tray")
		}
		// #nosec G204 -- path was resolved by LookPath for the fixed wl-copy program.
		command = exec.CommandContext(ctx, path)
	}
	command.Stdin = bytes.NewBufferString(value)
	return command.Run()
}

func openDoctorReport(ctx context.Context, options defaultModeOptions) {
	// #nosec G204 -- executable is the running Dropserve binary and arguments are fixed local paths.
	command := exec.CommandContext(ctx, options.Executable, "doctor", "--config", options.ConfigPath, "--state", options.StatePath)
	output, err := command.CombinedOutput()
	if err != nil && len(output) == 0 {
		output = []byte(fmt.Sprintf("Dropserve doctor could not run: %v\n", err))
	}
	directory, err := os.UserCacheDir()
	if err != nil {
		return
	}
	directory = filepath.Join(directory, "Dropserve")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return
	}
	reportPath := filepath.Join(directory, "doctor.txt")
	if err := os.WriteFile(reportPath, output, 0o600); err != nil {
		return
	}
	_ = launch.OpenPath(reportPath)
}
