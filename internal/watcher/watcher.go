// Package watcher turns filesystem notifications into debounced reconcile calls.
package watcher

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	defaultDebounce          = 500 * time.Millisecond
	defaultReconcileInterval = 30 * time.Second
	defaultMaximumDepth      = 3
	defaultWatchesPerApp     = 256
)

var ignoredDirectories = map[string]struct{}{
	".git":         {},
	".venv":        {},
	"__pycache__":  {},
	"dist":         {},
	"node_modules": {},
	"vendor":       {},
	"venv":         {},
}

// Options controls live filesystem reconciliation.
type Options struct {
	Roots             []string
	Debounce          time.Duration
	ReconcileInterval time.Duration
	MaximumDepth      int
	WatchesPerApp     int
	Reconcile         func() error
}

// Watcher owns one native watcher and its reconciliation goroutine.
type Watcher struct {
	native  *fsnotify.Watcher
	options Options
	roots   []string
	watched map[string]struct{}
	done    chan struct{}
	closed  chan struct{}
	once    sync.Once
}

// New starts watching all existing configured roots.
func New(options Options) (*Watcher, error) {
	if options.Reconcile == nil {
		return nil, errors.New("watcher reconcile callback is required")
	}
	if options.Debounce <= 0 {
		options.Debounce = defaultDebounce
	}
	if options.ReconcileInterval <= 0 {
		options.ReconcileInterval = defaultReconcileInterval
	}
	if options.MaximumDepth <= 0 {
		options.MaximumDepth = defaultMaximumDepth
	}
	if options.WatchesPerApp <= 0 {
		options.WatchesPerApp = defaultWatchesPerApp
	}

	native, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	watcher := &Watcher{
		native:  native,
		options: options,
		watched: make(map[string]struct{}),
		done:    make(chan struct{}),
		closed:  make(chan struct{}),
	}
	for _, configuredRoot := range options.Roots {
		root, absoluteErr := filepath.Abs(configuredRoot)
		if absoluteErr != nil {
			_ = native.Close()
			return nil, absoluteErr
		}
		watcher.roots = append(watcher.roots, filepath.Clean(root))
	}
	if err := watcher.refreshWatches(); err != nil {
		_ = native.Close()
		return nil, err
	}
	go watcher.run()
	return watcher, nil
}

// Close stops native watching and waits for the reconciliation goroutine.
func (watcher *Watcher) Close() error {
	watcher.once.Do(func() {
		close(watcher.done)
	})
	<-watcher.closed
	return nil
}

func (watcher *Watcher) run() {
	defer close(watcher.closed)
	defer func() {
		_ = watcher.native.Close()
	}()

	ticker := time.NewTicker(watcher.options.ReconcileInterval)
	defer ticker.Stop()
	var debounce *time.Timer
	var debounceChannel <-chan time.Time
	stopDebounce := func() {
		if debounce != nil && !debounce.Stop() {
			select {
			case <-debounce.C:
			default:
			}
		}
	}
	defer stopDebounce()

	for {
		select {
		case <-watcher.done:
			return
		case _, open := <-watcher.native.Errors:
			if !open {
				return
			}
			debounce, debounceChannel = resetTimer(debounce, watcher.options.Debounce)
		case event, open := <-watcher.native.Events:
			if !open {
				return
			}
			if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) == 0 {
				continue
			}
			debounce, debounceChannel = resetTimer(debounce, watcher.options.Debounce)
		case <-debounceChannel:
			debounceChannel = nil
			watcher.reconcile()
		case <-ticker.C:
			watcher.reconcile()
		}
	}
}

func resetTimer(timer *time.Timer, duration time.Duration) (*time.Timer, <-chan time.Time) {
	if timer == nil {
		timer = time.NewTimer(duration)
		return timer, timer.C
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(duration)
	return timer, timer.C
}

func (watcher *Watcher) reconcile() {
	if err := watcher.options.Reconcile(); err != nil {
		return
	}
	_ = watcher.refreshWatches()
}

func (watcher *Watcher) refreshWatches() error {
	for _, root := range watcher.roots {
		info, err := os.Stat(root)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() {
			continue
		}
		if err := watcher.add(root); err != nil {
			return err
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if !entry.IsDir() || ignoredDirectory(entry.Name()) {
				continue
			}
			if err := watcher.addAppTree(filepath.Join(root, entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

func (watcher *Watcher) addAppTree(appRoot string) error {
	watchCount := 0
	return filepath.WalkDir(appRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			return nil
		}
		if path != appRoot && ignoredDirectory(entry.Name()) {
			return filepath.SkipDir
		}
		relative, err := filepath.Rel(appRoot, path)
		if err != nil {
			return err
		}
		depth := 0
		if relative != "." {
			depth = strings.Count(filepath.ToSlash(relative), "/") + 1
		}
		if depth > watcher.options.MaximumDepth {
			return filepath.SkipDir
		}
		if watchCount >= watcher.options.WatchesPerApp {
			return filepath.SkipDir
		}
		if err := watcher.add(path); err != nil {
			return err
		}
		watchCount++
		return nil
	})
}

func (watcher *Watcher) add(path string) error {
	clean := filepath.Clean(path)
	if _, exists := watcher.watched[clean]; exists {
		return nil
	}
	if err := watcher.native.Add(clean); err != nil {
		return err
	}
	watcher.watched[clean] = struct{}{}
	return nil
}

func ignoredDirectory(name string) bool {
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
		return true
	}
	_, ignored := ignoredDirectories[strings.ToLower(name)]
	return ignored
}
