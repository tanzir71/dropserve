//go:build windows

package autostart

import (
	"encoding/xml"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestTaskXMLUsesSafePerUserLogonSettings(t *testing.T) {
	t.Parallel()

	data, err := makeTaskXML(
		`WORKSTATION\casey`,
		`C:\Program Files\Dropserve & Tools\dropserve.exe`,
	)
	if err != nil {
		t.Fatalf("make task XML: %v", err)
	}
	var document any
	if err := xml.Unmarshal(data, &document); err != nil {
		t.Fatalf("task XML is invalid: %v\n%s", err, data)
	}

	xmlText := string(data)
	wants := []string{
		`<LogonTrigger>`,
		`<UserId>WORKSTATION\casey</UserId>`,
		`<Delay>PT10S</Delay>`,
		`<LogonType>InteractiveToken</LogonType>`,
		`<RunLevel>LeastPrivilege</RunLevel>`,
		`<ExecutionTimeLimit>PT0S</ExecutionTimeLimit>`,
		`<DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>`,
		`<StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>`,
		`<Interval>PT1M</Interval>`,
		`<Count>3</Count>`,
		`<Command>C:\Program Files\Dropserve &amp; Tools\dropserve.exe</Command>`,
		`<Arguments>--background</Arguments>`,
	}
	for _, want := range wants {
		if !strings.Contains(xmlText, want) {
			t.Errorf("task XML does not contain %q:\n%s", want, xmlText)
		}
	}
	if strings.Contains(xmlText, "HighestAvailable") {
		t.Fatalf("task XML requests elevation:\n%s", xmlText)
	}
}

func TestTaskFileUsesUTF16LittleEndian(t *testing.T) {
	t.Parallel()

	data := encodeTaskFile([]byte(`<Task />`))
	if len(data) < 4 || data[0] != 0xff || data[1] != 0xfe {
		t.Fatalf("task file does not start with a UTF-16LE byte-order mark: %x", data)
	}
	decoded := make([]uint16, 0, (len(data)-2)/2)
	for index := 2; index+1 < len(data); index += 2 {
		decoded = append(decoded, uint16(data[index])|uint16(data[index+1])<<8)
	}
	text := string(utf16.Decode(decoded))
	if !strings.HasPrefix(text, `<?xml version="1.0" encoding="UTF-16"?>`) || !strings.HasSuffix(text, `<Task />`) {
		t.Fatalf("decoded task file = %q", text)
	}
}
