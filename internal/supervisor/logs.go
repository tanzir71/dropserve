package supervisor

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

const (
	memoryLogBytes = 256 << 10
	logFileBytes   = 1 << 20
	logFileCount   = 5
)

type logSink struct {
	ring *ringBuffer
	disk *rotatingLog
}

func newLogSink(directory, slug string) (*logSink, error) {
	sink := &logSink{ring: newRingBuffer(memoryLogBytes)}
	if directory == "" {
		return sink, nil
	}
	disk, err := newRotatingLog(directory, slug+".log")
	if err != nil {
		return nil, err
	}
	sink.disk = disk
	return sink, nil
}

func (sink *logSink) Write(content []byte) (int, error) {
	_, _ = sink.ring.Write(content)
	if sink.disk == nil {
		return len(content), nil
	}
	return sink.disk.Write(content)
}

func (sink *logSink) Close() error {
	if sink.disk == nil {
		return nil
	}
	return sink.disk.Close()
}

type rotatingLog struct {
	mu   sync.Mutex
	path string
	file *os.File
	size int64
}

func newRotatingLog(directory, name string) (*rotatingLog, error) {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(directory, name)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) // #nosec G304 -- path is the configured state directory plus a scanner-owned slug.
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	log := &rotatingLog{path: path, file: file, size: info.Size()}
	if log.size >= logFileBytes {
		if err := log.rotate(); err != nil {
			return nil, err
		}
	}
	return log, nil
}

func (log *rotatingLog) Write(content []byte) (int, error) {
	log.mu.Lock()
	defer log.mu.Unlock()
	total := 0
	for len(content) != 0 {
		if log.size >= logFileBytes {
			if err := log.rotate(); err != nil {
				return total, err
			}
		}
		remaining := int64(logFileBytes) - log.size
		chunkLength := len(content)
		if int64(chunkLength) > remaining {
			chunkLength = int(remaining)
		}
		written, err := log.file.Write(content[:chunkLength])
		total += written
		log.size += int64(written)
		content = content[written:]
		if err != nil {
			return total, err
		}
		if written == 0 {
			return total, io.ErrShortWrite
		}
	}
	return total, nil
}

func (log *rotatingLog) Close() error {
	log.mu.Lock()
	defer log.mu.Unlock()
	if log.file == nil {
		return nil
	}
	err := log.file.Close()
	log.file = nil
	return err
}

func (log *rotatingLog) rotate() error {
	if log.file != nil {
		if err := log.file.Close(); err != nil {
			return err
		}
		log.file = nil
	}
	oldest := log.path + "." + strconv.Itoa(logFileCount-1)
	if err := os.Remove(oldest); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for index := logFileCount - 2; index >= 1; index-- {
		from := log.path + "." + strconv.Itoa(index)
		to := log.path + "." + strconv.Itoa(index+1)
		if err := os.Rename(from, to); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := os.Rename(log.path, log.path+".1"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := os.OpenFile(log.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600) // #nosec G304 -- path was constructed from the configured state directory and scanner-owned slug.
	if err != nil {
		return err
	}
	log.file = file
	log.size = 0
	return nil
}
