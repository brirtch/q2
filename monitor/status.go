package monitor

import (
	"sync"
	"time"
)

// Activity represents a single monitoring activity.
type Activity struct {
	Timestamp time.Time `json:"timestamp"`
	Action    string    `json:"action"`
	Path      string    `json:"path"`
	Details   string    `json:"details,omitempty"`
}

// Stats holds monitoring statistics.
type Stats struct {
	FilesIndexed int64     `json:"files_indexed"`
	Errors       int64     `json:"errors"`
	StartedAt    time.Time `json:"started_at"`
}

// Status represents the current monitoring status.
type Status struct {
	State            string     `json:"status"`
	RecentActivities []Activity `json:"recent_activities"`
	Stats            Stats      `json:"stats"`
}

// StatusTracker tracks the current monitoring status and recent activities.
type StatusTracker struct {
	mu               sync.RWMutex
	state            string
	recentActivities []Activity
	maxActivities    int
	stats            Stats
}

// NewStatusTracker creates a new status tracker.
func NewStatusTracker() *StatusTracker {
	return &StatusTracker{
		state:            "starting",
		recentActivities: make([]Activity, 0, 100),
		maxActivities:    100,
		stats: Stats{
			StartedAt: time.Now(),
		},
	}
}

// SetState sets the current state (e.g., "running", "stopping", "stopped").
func (s *StatusTracker) SetState(state string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = state
}

// AddActivity adds an activity to the recent activities list.
func (s *StatusTracker) AddActivity(action, path, details string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	activity := Activity{
		Timestamp: time.Now(),
		Action:    action,
		Path:      path,
		Details:   details,
	}

	// Add to front of list
	s.recentActivities = append([]Activity{activity}, s.recentActivities...)

	// Trim to max size
	if len(s.recentActivities) > s.maxActivities {
		s.recentActivities = s.recentActivities[:s.maxActivities]
	}
}

// IncrementFilesIndexed increments the files indexed counter.
func (s *StatusTracker) IncrementFilesIndexed(count int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stats.FilesIndexed += count
}

// IncrementErrors increments the error counter.
func (s *StatusTracker) IncrementErrors() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stats.Errors++
}

// GetStatus returns the current status snapshot.
func (s *StatusTracker) GetStatus() Status {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Make a copy of recent activities
	activities := make([]Activity, len(s.recentActivities))
	copy(activities, s.recentActivities)

	return Status{
		State:            s.state,
		RecentActivities: activities,
		Stats:            s.stats,
	}
}
