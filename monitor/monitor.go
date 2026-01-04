package monitor

import (
	"jukel.org/q2/db"
)

// Monitor coordinates file watching.
type Monitor struct {
	Status  *StatusTracker
	watcher *Watcher
}

// New creates a new Monitor instance.
func New(database *db.DB) (*Monitor, error) {
	status := NewStatusTracker()

	watcher, err := NewWatcher(database, status)
	if err != nil {
		return nil, err
	}

	return &Monitor{
		Status:  status,
		watcher: watcher,
	}, nil
}

// Start begins monitoring.
func (m *Monitor) Start() error {
	m.Status.SetState("running")

	// Start the file watcher
	if err := m.watcher.Start(); err != nil {
		m.Status.SetState("error")
		return err
	}

	m.Status.AddActivity("started", "", "Monitoring started")
	return nil
}

// Stop stops monitoring gracefully.
func (m *Monitor) Stop() {
	m.Status.SetState("stopping")
	m.Status.AddActivity("stopping", "", "Shutting down monitoring")

	m.watcher.Stop()

	m.Status.SetState("stopped")
}

// AddFolder adds a new folder to be monitored.
func (m *Monitor) AddFolder(path string) error {
	return m.watcher.AddFolder(path)
}

// RemoveFolder removes a folder from monitoring.
func (m *Monitor) RemoveFolder(path string) error {
	return m.watcher.RemoveFolder(path)
}
