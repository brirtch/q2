package monitor

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"jukel.org/q2/db"
	"jukel.org/q2/scanner"
)

// FileEvent represents a file system event to be processed.
type FileEvent struct {
	Path      string
	Op        fsnotify.Op
	Timestamp time.Time
}

// Watcher watches monitored folders for file system changes.
type Watcher struct {
	db            *db.DB
	status        *StatusTracker
	fsWatcher     *fsnotify.Watcher
	done          chan struct{}
	wg            sync.WaitGroup
	debounceTime  time.Duration
	pendingEvents map[string]*FileEvent
	pendingMu     sync.Mutex
}

// NewWatcher creates a new file system watcher.
func NewWatcher(database *db.DB, status *StatusTracker) (*Watcher, error) {
	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	return &Watcher{
		db:            database,
		status:        status,
		fsWatcher:     fsWatcher,
		done:          make(chan struct{}),
		debounceTime:  100 * time.Millisecond,
		pendingEvents: make(map[string]*FileEvent),
	}, nil
}

// normalizePath applies platform-specific path normalization.
func normalizePath(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}

// Start begins watching all monitored folders.
func (w *Watcher) Start() error {
	// Start the event processing goroutines first
	w.wg.Add(3)
	go w.processEvents()
	go w.debounceEvents()

	// Add folder watches asynchronously to avoid blocking startup
	go func() {
		defer w.wg.Done()

		// Get all monitored folders
		rows, err := w.db.Query("SELECT path FROM folders")
		if err != nil {
			w.status.AddActivity("error", "", "Failed to query folders: "+err.Error())
			w.status.IncrementErrors()
			return
		}
		defer rows.Close()

		var folders []string
		for rows.Next() {
			var path string
			if err := rows.Scan(&path); err != nil {
				continue
			}
			folders = append(folders, path)
		}

		if rows.Err() != nil {
			return
		}

		// Add watchers for each folder recursively
		for _, folder := range folders {
			select {
			case <-w.done:
				return
			default:
			}
			if err := w.addWatchRecursive(folder); err != nil {
				w.status.AddActivity("error", folder, "Failed to watch: "+err.Error())
				w.status.IncrementErrors()
			}
		}
	}()

	return nil
}

// addWatchRecursive adds a watch on a directory and all its subdirectories.
func (w *Watcher) addWatchRecursive(path string) error {
	return filepath.Walk(path, func(walkPath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip inaccessible paths
		}
		if info.IsDir() {
			if err := w.fsWatcher.Add(walkPath); err != nil {
				return nil // Skip directories we can't watch
			}
		}
		return nil
	})
}

// AddFolder adds a new folder to be watched.
func (w *Watcher) AddFolder(path string) error {
	return w.addWatchRecursive(path)
}

// RemoveFolder removes a folder from being watched.
func (w *Watcher) RemoveFolder(path string) error {
	// fsnotify doesn't have a recursive remove, so we need to walk and remove each
	return filepath.Walk(path, func(walkPath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			w.fsWatcher.Remove(walkPath)
		}
		return nil
	})
}

// processEvents reads from fsnotify and queues events for debouncing.
func (w *Watcher) processEvents() {
	defer w.wg.Done()

	for {
		select {
		case <-w.done:
			return
		case event, ok := <-w.fsWatcher.Events:
			if !ok {
				return
			}
			w.queueEvent(event)
		case err, ok := <-w.fsWatcher.Errors:
			if !ok {
				return
			}
			w.status.AddActivity("error", "", "Watcher error: "+err.Error())
			w.status.IncrementErrors()
		}
	}
}

// queueEvent adds an event to the pending events map for debouncing.
func (w *Watcher) queueEvent(event fsnotify.Event) {
	w.pendingMu.Lock()
	defer w.pendingMu.Unlock()

	path := normalizePath(event.Name)
	w.pendingEvents[path] = &FileEvent{
		Path:      event.Name,
		Op:        event.Op,
		Timestamp: time.Now(),
	}
}

// debounceEvents periodically flushes debounced events to the events channel.
func (w *Watcher) debounceEvents() {
	defer w.wg.Done()

	ticker := time.NewTicker(w.debounceTime)
	defer ticker.Stop()

	for {
		select {
		case <-w.done:
			return
		case <-ticker.C:
			w.flushPendingEvents()
		}
	}
}

// flushPendingEvents processes pending events that have been debounced.
func (w *Watcher) flushPendingEvents() {
	w.pendingMu.Lock()
	defer w.pendingMu.Unlock()

	now := time.Now()
	for path, event := range w.pendingEvents {
		if now.Sub(event.Timestamp) >= w.debounceTime {
			w.processEvent(*event)
			delete(w.pendingEvents, path)
		}
	}
}

// processEvent handles a single file event.
func (w *Watcher) processEvent(event FileEvent) {
	path := event.Path
	normalizedPath := normalizePath(path)

	// Find the parent monitored folder
	_, folderID, err := scanner.FindParentFolder(w.db, path)
	if err != nil {
		// File is not in a monitored folder
		return
	}

	switch {
	case event.Op&fsnotify.Remove == fsnotify.Remove:
		// File was removed
		result := w.db.Write("DELETE FROM files WHERE path = ?", normalizedPath)
		if result.Err == nil && result.RowsAffected > 0 {
			w.status.AddActivity("removed", path, "")
		}

	case event.Op&fsnotify.Create == fsnotify.Create, event.Op&fsnotify.Write == fsnotify.Write:
		// File was created or modified
		info, err := os.Stat(path)
		if err != nil {
			return // File may have been deleted already
		}

		if info.IsDir() {
			// New directory - add it to the watcher
			w.fsWatcher.Add(path)
			return
		}

		// Index the file
		result, err := scanner.ScanFolder(w.db, filepath.Dir(path), folderID)
		if err == nil && result.FilesAdded > 0 {
			w.status.AddActivity("indexed", path, "")
			w.status.IncrementFilesIndexed(int64(result.FilesAdded))
		} else if err == nil && result.FilesUpdated > 0 {
			w.status.AddActivity("updated", path, "")
		}

	case event.Op&fsnotify.Rename == fsnotify.Rename:
		// File was renamed - treat as remove (the new name will trigger a Create)
		result := w.db.Write("DELETE FROM files WHERE path = ?", normalizedPath)
		if result.Err == nil && result.RowsAffected > 0 {
			w.status.AddActivity("removed", path, "renamed")
		}
	}
}

// Stop stops the watcher.
func (w *Watcher) Stop() {
	close(w.done)
	w.fsWatcher.Close()
	w.wg.Wait()
}
