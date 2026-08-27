package autostart

import (
	"fmt"
	"strconv"
	"strings"
)

func makeSystemdUnit(executable string) []byte {
	executable = strings.ReplaceAll(executable, "%", "%%")
	return []byte(fmt.Sprintf(`[Unit]
Description=Dropserve local app server
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
ExecStart=%s --background
Restart=on-failure
RestartSec=60s

[Install]
WantedBy=default.target
`, strconv.Quote(executable)))
}
