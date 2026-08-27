package supervisor

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestTenMegabyteLogBurstIsMemoryBoundedAndRotatesDisk(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	sink, err := newLogSink(directory, "burst")
	if err != nil {
		t.Fatalf("create bounded log sink: %v", err)
	}
	defer func() {
		if closeErr := sink.Close(); closeErr != nil {
			t.Errorf("close bounded log sink: %v", closeErr)
		}
	}()
	burst := bytes.Repeat([]byte("0123456789abcdef"), (10<<20)/16)
	written, err := sink.Write(burst)
	if err != nil {
		t.Fatalf("write 10 MB log burst: %v", err)
	}
	if written != len(burst) {
		t.Fatalf("log write = %d bytes, want %d", written, len(burst))
	}
	if memoryBytes := sink.ring.Len(); memoryBytes != 256<<10 {
		t.Fatalf("in-memory log bytes = %d, want bounded 256 KB", memoryBytes)
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read rotating logs: %v", err)
	}
	var logFiles int
	var diskBytes int64
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "burst.log") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			t.Fatalf("inspect %s: %v", entry.Name(), err)
		}
		logFiles++
		diskBytes += info.Size()
		if info.Size() > 1<<20 {
			t.Fatalf("rotated file %s = %d bytes, exceeds 1 MB", entry.Name(), info.Size())
		}
	}
	if logFiles != 5 {
		t.Fatalf("rotating log file count = %d, want 5", logFiles)
	}
	if diskBytes > 5<<20 {
		t.Fatalf("rotating logs use %d bytes, exceed 5 MB", diskBytes)
	}
}
