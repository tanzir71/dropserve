//go:build windows

// Package autostart manages Dropserve's per-user Windows Scheduled Task.
package autostart

import (
	"context"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"time"
	"unicode/utf16"
)

const taskName = "Dropserve"

type taskDocument struct {
	XMLName          xml.Name         `xml:"Task"`
	Version          string           `xml:"version,attr"`
	Namespace        string           `xml:"xmlns,attr"`
	RegistrationInfo registrationInfo `xml:"RegistrationInfo"`
	Triggers         taskTriggers     `xml:"Triggers"`
	Principals       taskPrincipals   `xml:"Principals"`
	Settings         taskSettings     `xml:"Settings"`
	Actions          taskActions      `xml:"Actions"`
}

type registrationInfo struct {
	Description string `xml:"Description"`
}

type taskTriggers struct {
	Logon taskLogonTrigger `xml:"LogonTrigger"`
}

type taskLogonTrigger struct {
	Enabled bool   `xml:"Enabled"`
	UserID  string `xml:"UserId"`
	Delay   string `xml:"Delay"`
}

type taskPrincipals struct {
	Principal taskPrincipal `xml:"Principal"`
}

type taskPrincipal struct {
	ID        string `xml:"id,attr"`
	UserID    string `xml:"UserId"`
	LogonType string `xml:"LogonType"`
	RunLevel  string `xml:"RunLevel"`
}

type taskSettings struct {
	MultipleInstancesPolicy    string               `xml:"MultipleInstancesPolicy"`
	DisallowStartIfOnBatteries bool                 `xml:"DisallowStartIfOnBatteries"`
	StopIfGoingOnBatteries     bool                 `xml:"StopIfGoingOnBatteries"`
	AllowHardTerminate         bool                 `xml:"AllowHardTerminate"`
	StartWhenAvailable         bool                 `xml:"StartWhenAvailable"`
	RunOnlyIfNetworkAvailable  bool                 `xml:"RunOnlyIfNetworkAvailable"`
	AllowStartOnDemand         bool                 `xml:"AllowStartOnDemand"`
	Enabled                    bool                 `xml:"Enabled"`
	Hidden                     bool                 `xml:"Hidden"`
	RunOnlyIfIdle              bool                 `xml:"RunOnlyIfIdle"`
	WakeToRun                  bool                 `xml:"WakeToRun"`
	ExecutionTimeLimit         string               `xml:"ExecutionTimeLimit"`
	Priority                   int                  `xml:"Priority"`
	RestartOnFailure           taskRestartOnFailure `xml:"RestartOnFailure"`
}

type taskRestartOnFailure struct {
	Interval string `xml:"Interval"`
	Count    int    `xml:"Count"`
}

type taskActions struct {
	Context string         `xml:"Context,attr"`
	Exec    taskExecAction `xml:"Exec"`
}

type taskExecAction struct {
	Command   string `xml:"Command"`
	Arguments string `xml:"Arguments"`
}

// Enable creates or replaces the current user's Dropserve logon task.
func Enable(executable string) error {
	currentUser, err := user.Current()
	if err != nil {
		return fmt.Errorf("find the current Windows user: %w", err)
	}
	taskXML, err := makeTaskXML(currentUser.Username, executable)
	if err != nil {
		return err
	}

	taskFile, err := os.CreateTemp("", "dropserve-autostart-*.xml")
	if err != nil {
		return fmt.Errorf("create temporary task XML: %w", err)
	}
	taskPath := taskFile.Name()
	defer func() {
		_ = os.Remove(taskPath)
	}()
	if _, err := taskFile.Write(encodeTaskFile(taskXML)); err != nil {
		_ = taskFile.Close()
		return fmt.Errorf("write temporary task XML: %w", err)
	}
	if err := taskFile.Close(); err != nil {
		return fmt.Errorf("close temporary task XML: %w", err)
	}

	output, err := runTaskCommand("/Create", "/XML", taskPath, "/TN", taskName, "/F")
	if err != nil {
		return commandError("create the Windows Scheduled Task", output, err)
	}
	return verifyEnabled("Windows Scheduled Task", Enabled)
}

// Disable removes the current user's Dropserve logon task. It is idempotent.
func Disable() error {
	enabled, err := Enabled()
	if err != nil {
		return err
	}
	if !enabled {
		return nil
	}

	output, deleteErr := runTaskCommand("/Delete", "/TN", taskName, "/F")
	if deleteErr == nil {
		return nil
	}
	enabled, statusErr := Enabled()
	if statusErr == nil && !enabled {
		return nil
	}
	return commandError("delete the Windows Scheduled Task", output, deleteErr)
}

// Enabled reports the actual presence of Dropserve's Windows Scheduled Task.
func Enabled() (bool, error) {
	output, err := runTaskCommand("/Query", "/TN", taskName, "/XML")
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return false, nil
	}
	return false, commandError("query the Windows Scheduled Task", output, err)
}

func makeTaskXML(userName, executable string) ([]byte, error) {
	document := taskDocument{
		Version:   "1.4",
		Namespace: "http://schemas.microsoft.com/windows/2004/02/mit/task",
		RegistrationInfo: registrationInfo{
			Description: "Start Dropserve when this user logs on.",
		},
		Triggers: taskTriggers{Logon: taskLogonTrigger{
			Enabled: true,
			UserID:  userName,
			Delay:   "PT10S",
		}},
		Principals: taskPrincipals{Principal: taskPrincipal{
			ID:        "Author",
			UserID:    userName,
			LogonType: "InteractiveToken",
			RunLevel:  "LeastPrivilege",
		}},
		Settings: taskSettings{
			MultipleInstancesPolicy:    "IgnoreNew",
			DisallowStartIfOnBatteries: false,
			StopIfGoingOnBatteries:     false,
			AllowHardTerminate:         true,
			StartWhenAvailable:         true,
			RunOnlyIfNetworkAvailable:  false,
			AllowStartOnDemand:         true,
			Enabled:                    true,
			Hidden:                     false,
			RunOnlyIfIdle:              false,
			WakeToRun:                  false,
			ExecutionTimeLimit:         "PT0S",
			Priority:                   7,
			RestartOnFailure: taskRestartOnFailure{
				Interval: "PT1M",
				Count:    3,
			},
		},
		Actions: taskActions{
			Context: "Author",
			Exec: taskExecAction{
				Command:   executable,
				Arguments: "--background",
			},
		},
	}
	data, err := xml.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode Windows Scheduled Task XML: %w", err)
	}
	return data, nil
}

func encodeTaskFile(taskXML []byte) []byte {
	const header = `<?xml version="1.0" encoding="UTF-16"?>` + "\r\n"
	encoded := utf16.Encode([]rune(header + string(taskXML)))
	data := make([]byte, 2+(len(encoded)*2))
	data[0] = 0xff
	data[1] = 0xfe
	for index, codeUnit := range encoded {
		binary.LittleEndian.PutUint16(data[2+(index*2):], codeUnit)
	}
	return data
}

func runTaskCommand(arguments ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "schtasks.exe", arguments...) // #nosec G204 -- executable is fixed and arguments are package-controlled task operations and paths.
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		return output, fmt.Errorf("schtasks timed out: %w", ctx.Err())
	}
	return output, err
}

func commandError(action string, output []byte, err error) error {
	detail := strings.TrimSpace(string(output))
	if detail == "" {
		return fmt.Errorf("%s: %w", action, err)
	}
	return fmt.Errorf("%s: %w: %s", action, err, detail)
}
