// Package cast provides Chromecast device discovery and media control.
package cast

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/vishen/go-chromecast/application"
	"github.com/vishen/go-chromecast/dns"
)

// Device represents a discovered Chromecast device.
type Device struct {
	UUID       string `json:"uuid"`
	Name       string `json:"name"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	DeviceType string `json:"device_type"`
	IsAudio    bool   `json:"is_audio"`
}

// audioDeviceTypes contains device types that are audio-only (speakers).
var audioDeviceTypes = map[string]bool{
	"Google Home":          true,
	"Google Home Mini":     true,
	"Google Nest Mini":     true,
	"Google Nest Audio":    true,
	"Google Home Max":      true,
	"Chromecast Audio":     true,
	"Google Cast Group":    true,
	"Lenovo Smart Clock":   true,
	"JBL Link":             true,
}

// isAudioDevice checks if a device type is an audio-only device.
func isAudioDevice(deviceType string) bool {
	// Check exact match first
	if audioDeviceTypes[deviceType] {
		return true
	}
	// Check for partial matches (device names can vary)
	lower := strings.ToLower(deviceType)
	if strings.Contains(lower, "speaker") ||
		strings.Contains(lower, "audio") ||
		strings.Contains(lower, "home mini") ||
		strings.Contains(lower, "nest mini") ||
		strings.Contains(lower, "nest audio") ||
		strings.Contains(lower, "cast group") {
		return true
	}
	return false
}

// Status represents the current playback status.
type Status struct {
	Connected   bool    `json:"connected"`
	DeviceName  string  `json:"device_name,omitempty"`
	MediaURL    string  `json:"media_url,omitempty"`
	MediaTitle  string  `json:"media_title,omitempty"`
	PlayerState string  `json:"player_state,omitempty"` // IDLE, BUFFERING, PLAYING, PAUSED
	CurrentTime float64 `json:"current_time"`
	Duration    float64 `json:"duration"`
	Volume      float64 `json:"volume"`
	Muted       bool    `json:"muted"`
}

// Manager handles Chromecast device discovery and control.
type Manager struct {
	mu          sync.RWMutex
	devices     map[string]*Device
	app         *application.Application
	connectedTo *Device
	baseURL     string // Base URL for media streaming (e.g., "http://192.168.1.100:8090")
}

// NewManager creates a new cast manager.
func NewManager(baseURL string) *Manager {
	return &Manager{
		devices: make(map[string]*Device),
		baseURL: baseURL,
	}
}

// SetBaseURL updates the base URL for media streaming.
func (m *Manager) SetBaseURL(baseURL string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.baseURL = baseURL
}

// DiscoverDevices searches for Chromecast devices on the network.
func (m *Manager) DiscoverDevices(ctx context.Context, timeout time.Duration) ([]Device, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	entriesChan, err := dns.DiscoverCastDNSEntries(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("discovery failed: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Clear old devices
	m.devices = make(map[string]*Device)

	var result []Device
	for entry := range entriesChan {
		device := &Device{
			UUID:       entry.UUID,
			Name:       entry.DeviceName,
			Host:       entry.AddrV4.String(),
			Port:       entry.Port,
			DeviceType: entry.Device,
			IsAudio:    isAudioDevice(entry.Device),
		}
		m.devices[entry.UUID] = device
		result = append(result, *device)
	}

	return result, nil
}

// GetDevices returns the cached list of discovered devices.
func (m *Manager) GetDevices() []Device {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]Device, 0, len(m.devices))
	for _, d := range m.devices {
		result = append(result, *d)
	}
	return result
}

// Connect establishes a connection to a Chromecast device.
func (m *Manager) Connect(uuid string) error {
	m.mu.Lock()

	device, ok := m.devices[uuid]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("device not found: %s", uuid)
	}

	// Disconnect from current device if connected
	if m.app != nil {
		oldApp := m.app
		m.app = nil
		m.connectedTo = nil
		m.mu.Unlock()
		// Close in background to avoid blocking
		go oldApp.Close(false)
		m.mu.Lock()
	}

	// Store device info before releasing lock
	host := device.Host
	port := device.Port
	m.mu.Unlock()

	// Create new application connection with timeout
	app := application.NewApplication()

	errChan := make(chan error, 1)
	go func() {
		errChan <- app.Start(host, port)
	}()

	select {
	case err := <-errChan:
		if err != nil {
			return fmt.Errorf("failed to connect: %w", err)
		}
	case <-time.After(10 * time.Second):
		return fmt.Errorf("connection timed out after 10 seconds")
	}

	m.mu.Lock()
	m.app = app
	m.connectedTo = device
	m.mu.Unlock()

	return nil
}

// Disconnect closes the connection to the current device.
func (m *Manager) Disconnect() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.app != nil {
		m.app.Close(false)
		m.app = nil
		m.connectedTo = nil
	}
	return nil
}

// IsConnected returns true if connected to a device.
func (m *Manager) IsConnected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.app != nil && m.connectedTo != nil
}

// PlayMedia starts playing a media file on the connected device.
// The path should be the file path that will be appended to the base URL.
// Returns the URL that was sent to the Chromecast.
func (m *Manager) PlayMedia(filePath, contentType, title string) (string, error) {
	m.mu.Lock()

	if m.app == nil {
		m.mu.Unlock()
		return "", fmt.Errorf("not connected to any device")
	}

	if m.baseURL == "" {
		m.mu.Unlock()
		return "", fmt.Errorf("base URL not set - cannot construct media URL")
	}

	// Construct the full URL based on content type
	// Use PathEscape and replace + with %20 for better Chromecast compatibility
	encodedPath := strings.ReplaceAll(url.QueryEscape(filePath), "+", "%20")
	var mediaURL string
	if len(contentType) >= 5 && contentType[:5] == "video" {
		mediaURL = fmt.Sprintf("%s/api/video?path=%s", m.baseURL, encodedPath)
	} else if len(contentType) >= 5 && contentType[:5] == "image" {
		mediaURL = fmt.Sprintf("%s/api/image?path=%s", m.baseURL, encodedPath)
	} else {
		mediaURL = fmt.Sprintf("%s/api/stream?path=%s", m.baseURL, encodedPath)
	}

	fmt.Printf("Casting to %s: %s (%s)\n", m.connectedTo.Name, mediaURL, contentType)

	// Store app reference before releasing lock
	app := m.app

	// Release lock before calling Load (it can block)
	m.mu.Unlock()

	// Load the media with a timeout using a channel
	errChan := make(chan error, 1)
	go func() {
		// Load: startTime=0, transcode=false, detach=false, forceDetach=false
		errChan <- app.Load(mediaURL, 0, contentType, false, false, false)
	}()

	// Wait for load with timeout
	select {
	case err := <-errChan:
		if err != nil {
			return mediaURL, fmt.Errorf("failed to load media: %w", err)
		}
	case <-time.After(10 * time.Second):
		return mediaURL, fmt.Errorf("load timed out after 10 seconds")
	}

	return mediaURL, nil
}

// PlayURL plays a direct URL on the connected device.
func (m *Manager) PlayURL(mediaURL, contentType string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.app == nil {
		return fmt.Errorf("not connected to any device")
	}

	if err := m.app.Load(mediaURL, 0, contentType, false, true, true); err != nil {
		return fmt.Errorf("failed to load media: %w", err)
	}

	return nil
}

// Pause pauses the current playback.
func (m *Manager) Pause() error {
	m.mu.Lock()
	if m.app == nil {
		m.mu.Unlock()
		return fmt.Errorf("not connected to any device")
	}
	app := m.app
	m.mu.Unlock()

	return app.Pause()
}

// Resume resumes playback.
func (m *Manager) Resume() error {
	m.mu.Lock()
	if m.app == nil {
		m.mu.Unlock()
		return fmt.Errorf("not connected to any device")
	}
	app := m.app
	m.mu.Unlock()

	return app.Unpause()
}

// Stop stops the current playback.
func (m *Manager) Stop() error {
	m.mu.Lock()
	if m.app == nil {
		m.mu.Unlock()
		return fmt.Errorf("not connected to any device")
	}
	app := m.app
	m.mu.Unlock()

	return app.Stop()
}

// Seek seeks to a specific position in seconds.
func (m *Manager) Seek(position float64) error {
	m.mu.Lock()
	if m.app == nil {
		m.mu.Unlock()
		return fmt.Errorf("not connected to any device")
	}
	app := m.app
	m.mu.Unlock()

	return app.Seek(int(position))
}

// SetVolume sets the volume level (0.0 to 1.0).
func (m *Manager) SetVolume(level float64) error {
	m.mu.Lock()
	if m.app == nil {
		m.mu.Unlock()
		return fmt.Errorf("not connected to any device")
	}
	app := m.app
	m.mu.Unlock()

	return app.SetVolume(float32(level))
}

// SetMuted sets the mute state.
func (m *Manager) SetMuted(muted bool) error {
	m.mu.Lock()
	if m.app == nil {
		m.mu.Unlock()
		return fmt.Errorf("not connected to any device")
	}
	app := m.app
	m.mu.Unlock()

	return app.SetMuted(muted)
}

// GetStatus returns the current playback status.
func (m *Manager) GetStatus() Status {
	m.mu.RLock()
	app := m.app
	connectedTo := m.connectedTo
	m.mu.RUnlock()

	status := Status{
		Connected: app != nil && connectedTo != nil,
	}

	if connectedTo != nil {
		status.DeviceName = connectedTo.Name
	}

	if app == nil {
		return status
	}

	// Force status update from device
	if err := app.Update(); err != nil {
		fmt.Printf("Cast update error: %v\n", err)
	}

	// Get cast status
	castStatus, media, volume := app.Status()

	// Debug: log what we're getting
	fmt.Printf("Cast status - castStatus: %v, media: %v, volume: %v\n", castStatus != nil, media != nil, volume != nil)

	// Get volume info
	if volume != nil {
		status.Volume = float64(volume.Level)
		status.Muted = volume.Muted
	}

	// Get media status
	if media != nil {
		status.PlayerState = media.PlayerState
		status.CurrentTime = float64(media.CurrentTime)
		status.Duration = float64(media.Media.Duration)
		status.MediaURL = media.Media.ContentId
		fmt.Printf("Media status - state: %s, time: %.1f, duration: %.1f\n", media.PlayerState, media.CurrentTime, media.Media.Duration)
	}

	return status
}

// ConnectedDevice returns the currently connected device, or nil if not connected.
func (m *Manager) ConnectedDevice() *Device {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connectedTo
}
