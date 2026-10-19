package config

import (
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const watchSettle = 200 * time.Millisecond

// Watcher tells when the user file or the project file of a folder changes
// on disk, by hand or from another mo. Check looks at them again on demand,
// for where the system does not report changes.
type Watcher struct {
	files   []string
	notify  *fsnotify.Watcher
	changed chan struct{}

	mu    sync.Mutex
	stamp string
}

// Watch starts watching the settings files of workDir. Changes says when
// one of them is written, created or removed.
func Watch(workDir string) (*Watcher, error) {
	notify, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &Watcher{
		files:   []string{UserFile(), ProjectFile(workDir), LegacyProjectFile(workDir)},
		notify:  notify,
		changed: make(chan struct{}, 1),
	}
	w.stamp = w.read()
	for _, dir := range []string{filepath.Dir(UserFile()), filepath.Dir(filepath.Dir(UserFile())), ProjectDir(workDir), workDir} {
		_ = notify.Add(dir)
	}
	go w.listen()
	return w, nil
}

func (w *Watcher) Changes() <-chan struct{} {
	return w.changed
}

// Check reports a change missed by the system's notices.
func (w *Watcher) Check() {
	if w.restamp() {
		w.report()
	}
}

func (w *Watcher) Close() error {
	return w.notify.Close()
}

func (w *Watcher) listen() {
	settle := time.NewTimer(watchSettle)
	settle.Stop()
	for {
		select {
		case event, open := <-w.notify.Events:
			if !open {
				return
			}
			w.followNewFolder(event)
			if slices.Contains(w.files, event.Name) {
				settle.Reset(watchSettle)
			}
		case _, open := <-w.notify.Errors:
			if !open {
				return
			}
		case <-settle.C:
			w.Check()
		}
	}
}

// followNewFolder starts watching the folder of a settings file once it is
// created, as when the first project setting is saved.
func (w *Watcher) followNewFolder(event fsnotify.Event) {
	if !event.Has(fsnotify.Create) {
		return
	}
	for _, file := range w.files {
		if filepath.Dir(file) == event.Name {
			_ = w.notify.Add(event.Name)
		}
	}
}

func (w *Watcher) report() {
	select {
	case w.changed <- struct{}{}:
	default:
	}
}

// restamp reads the files again and tells whether they changed since.
func (w *Watcher) restamp() bool {
	stamp := w.read()
	w.mu.Lock()
	defer w.mu.Unlock()
	if stamp == w.stamp {
		return false
	}
	w.stamp = stamp
	return true
}

// read is the files' contents, each after its path, to compare.
func (w *Watcher) read() string {
	var stamp []byte
	for _, file := range w.files {
		data, _ := os.ReadFile(file)
		stamp = append(stamp, file...)
		stamp = append(stamp, 0)
		stamp = append(stamp, digest(data)...)
	}
	return string(stamp)
}
