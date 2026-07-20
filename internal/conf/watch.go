package conf

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// DraftWatcher watches the filesystem source and emits a new hash after a
// debounced change. It never publishes automatically.
type DraftWatcher struct {
	DataDir  string
	Debounce time.Duration
}

// Watch uses fsnotify for normal local filesystems and falls back to polling
// when a watcher cannot be created (for example, some network filesystems).
func (w DraftWatcher) Watch(ctx context.Context) (<-chan string, <-chan error) {
	changes := make(chan string, 1)
	errs := make(chan error, 1)
	debounce := w.Debounce
	if debounce <= 0 {
		debounce = 500 * time.Millisecond
	}
	go func() {
		defer close(changes)
		defer close(errs)
		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			w.poll(ctx, debounce, changes, errs)
			return
		}
		defer func() { _ = watcher.Close() }()
		paths := []string{filepath.Join(w.DataDir, "config.d"), w.DataDir}
		for _, path := range paths {
			if err := os.MkdirAll(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
				select {
				case errs <- err:
				default:
				}
				return
			}
			if err := watcher.Add(path); err != nil {
				w.poll(ctx, debounce, changes, errs)
				return
			}
		}
		var last string
		var timer *time.Timer
		var timerC <-chan time.Time
		emit := func() {
			hash, err := DraftHash(w.DataDir)
			if err != nil {
				select {
				case errs <- err:
				default:
				}
				return
			}
			if last != "" && hash != last {
				select {
				case changes <- hash:
				default:
				}
			}
			last = hash
		}
		if hash, err := DraftHash(w.DataDir); err == nil {
			last = hash
		}
		for {
			select {
			case <-ctx.Done():
				if timer != nil {
					timer.Stop()
				}
				return
			case <-watcher.Errors:
				w.poll(ctx, debounce, changes, errs)
				return
			case _, ok := <-watcher.Events:
				if !ok {
					w.poll(ctx, debounce, changes, errs)
					return
				}
				if timer == nil {
					timer = time.NewTimer(debounce)
				} else {
					timer.Reset(debounce)
				}
				timerC = timer.C
			case <-timerC:
				emit()
				timerC = nil
			}
		}
	}()
	return changes, errs
}

func (w DraftWatcher) poll(ctx context.Context, interval time.Duration, changes chan<- string, errs chan<- error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var last string
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			hash, err := DraftHash(w.DataDir)
			if err != nil {
				select {
				case errs <- err:
				default:
				}
				continue
			}
			if last != "" && hash != last {
				select {
				case changes <- hash:
				default:
				}
			}
			last = hash
		}
	}
}

// WatchedPaths documents the source locations included by the watcher.
func WatchedPaths(dataDir string) []string {
	return []string{filepath.Join(dataDir, "config.d"), filepath.Join(dataDir, "native.yaml")}
}
