// Package cast provides Chromecast device discovery and media control.
package cast

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
	"github.com/vishen/go-chromecast/application"
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

// mdnsEntry accumulates DNS-SD records for a single Chromecast device instance.
type mdnsEntry struct {
	hostName string // from SRV Target
	host     string // resolved IPv4, filled in second pass
	port     int
	uuid     string
	name     string
	devType  string
}

func ensureMDNSEntry(m map[string]*mdnsEntry, key string) *mdnsEntry {
	if _, ok := m[key]; !ok {
		m[key] = &mdnsEntry{}
	}
	return m[key]
}

// discoverCastDevicesUnicast sends an mDNS PTR query from a random (non-5353) UDP port.
// Per RFC 6762 §6, devices MUST respond via unicast when the query source port is not 5353,
// so no multicast group membership is required. This reliably works on Windows where the
// grandcat/zeroconf library's multicast socket binding often fails due to binding to
// 224.0.0.0:5353 instead of 0.0.0.0:5353.
//
// To cover all subnets (WiFi, Ethernet, etc.), one socket is created per multicast-capable
// interface, each sending from that interface's IP. Unicast replies come back to each
// respective socket, which are all read concurrently and fed to a shared channel.
func discoverCastDevicesUnicast(ctx context.Context) ([]Device, error) {
	// Build a DNS PTR query for the Chromecast service type.
	msg := new(dns.Msg)
	msg.Id = dns.Id()
	msg.RecursionDesired = false
	msg.Question = []dns.Question{{
		Name:   "_googlecast._tcp.local.",
		Qtype:  dns.TypePTR,
		Qclass: dns.ClassINET,
	}}
	buf, err := msg.Pack()
	if err != nil {
		return nil, fmt.Errorf("pack dns msg: %w", err)
	}

	mdnsAddr := &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}

	// Collect all candidate interface IPs. Each gets its own socket so that
	// unicast replies (which go to the sender's IP:port) are received.
	type ifSocket struct {
		conn *net.UDPConn
	}
	var sockets []ifSocket

	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 ||
			iface.Flags&net.FlagMulticast == 0 ||
			iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip = ip.To4(); ip == nil {
				continue
			}
			// Bind to this specific interface IP so multicast sends go out the right NIC.
			c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: ip, Port: 0})
			if err != nil {
				continue
			}
			if deadline, ok := ctx.Deadline(); ok {
				c.SetDeadline(deadline)
			}
			c.WriteToUDP(buf, mdnsAddr)
			sockets = append(sockets, ifSocket{c})
		}
	}

	// Fallback: if no interface-specific sockets could be created, use a generic one.
	if len(sockets) == 0 {
		c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
		if err != nil {
			return nil, fmt.Errorf("udp listen: %w", err)
		}
		if deadline, ok := ctx.Deadline(); ok {
			c.SetDeadline(deadline)
		}
		c.WriteToUDP(buf, mdnsAddr)
		sockets = append(sockets, ifSocket{c})
	}

	defer func() {
		for _, s := range sockets {
			s.conn.Close()
		}
	}()

	// Read DNS responses from all sockets concurrently via a shared channel.
	type rawPacket struct{ data []byte }
	pktCh := make(chan rawPacket, 64)

	var wg sync.WaitGroup
	for _, s := range sockets {
		wg.Add(1)
		go func(c *net.UDPConn) {
			defer wg.Done()
			rbuf := make([]byte, 65536)
			for {
				n, _, err := c.ReadFromUDP(rbuf)
				if err != nil {
					return // deadline reached or socket closed
				}
				pkt := make([]byte, n)
				copy(pkt, rbuf[:n])
				select {
				case pktCh <- rawPacket{pkt}:
				case <-ctx.Done():
					return
				}
			}
		}(s.conn)
	}
	go func() {
		wg.Wait()
		close(pktCh)
	}()

	// sendQuery broadcasts a DNS message on all sockets.
	sendQuery := func(msg *dns.Msg) {
		b, err := msg.Pack()
		if err != nil {
			return
		}
		for _, s := range sockets {
			s.conn.WriteToUDP(b, mdnsAddr)
		}
	}

	// Resend the PTR query every second so devices that missed the first packet
	// still get a chance to respond.
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				sendQuery(msg)
			case <-ctx.Done():
				return
			}
		}
	}()

	// Accumulate DNS records until all readers finish (deadline hit).
	entries := make(map[string]*mdnsEntry) // instance FQDN → partial info
	hosts := make(map[string]string)       // hostname FQDN → IPv4

	for pkt := range pktCh {
		var resp dns.Msg
		if err := resp.Unpack(pkt.data); err != nil {
			continue
		}
		all := append(append(resp.Answer, resp.Ns...), resp.Extra...)
		for _, rr := range all {
			switch r := rr.(type) {
			case *dns.PTR:
				if r.Hdr.Name == "_googlecast._tcp.local." {
					if _, exists := entries[r.Ptr]; !exists {
						entries[r.Ptr] = &mdnsEntry{}
						// Send a targeted follow-up query for this instance's SRV+TXT
						// records so split responses (PTR in one packet, SRV/TXT in another)
						// are reliably received.
						followUp := new(dns.Msg)
						followUp.Id = dns.Id()
						followUp.RecursionDesired = false
						followUp.Question = []dns.Question{
							{Name: r.Ptr, Qtype: dns.TypeSRV, Qclass: dns.ClassINET},
							{Name: r.Ptr, Qtype: dns.TypeTXT, Qclass: dns.ClassINET},
						}
						sendQuery(followUp)
					}
				}
			case *dns.SRV:
				e := ensureMDNSEntry(entries, r.Hdr.Name)
				e.hostName = r.Target
				e.port = int(r.Port)
			case *dns.TXT:
				e := ensureMDNSEntry(entries, r.Hdr.Name)
				for _, txt := range r.Txt {
					kv := strings.SplitN(txt, "=", 2)
					if len(kv) != 2 {
						continue
					}
					switch kv[0] {
					case "id":
						e.uuid = kv[1]
					case "fn":
						e.name = kv[1]
					case "md":
						e.devType = kv[1]
					}
				}
			case *dns.A:
				hosts[r.Hdr.Name] = r.A.String()
			}
		}
	}

	// Resolve hostnames to IPs and build the Device list.
	const castSuffix = "._googlecast._tcp.local."
	var devices []Device
	for instanceFQDN, e := range entries {
		if e.host == "" && e.hostName != "" {
			e.host = hosts[e.hostName]
		}
		if e.host == "" || e.port == 0 {
			continue
		}
		name := e.name
		if name == "" {
			// TXT record fn field missing — extract name from the PTR instance label.
			name = strings.TrimSuffix(instanceFQDN, castSuffix)
		}
		devices = append(devices, Device{
			UUID:       e.uuid,
			Name:       name,
			Host:       e.host,
			Port:       e.port,
			DeviceType: e.devType,
			IsAudio:    isAudioDevice(e.devType),
		})
	}
	return devices, nil
}

// DiscoverDevices searches for Chromecast devices on the network using unicast mDNS
// (RFC 6762 §6). This approach is reliable on Windows where zeroconf's multicast
// socket binding fails.
func (m *Manager) DiscoverDevices(ctx context.Context, timeout time.Duration) ([]Device, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	devices, err := discoverCastDevicesUnicast(ctx)
	if err != nil {
		return nil, err
	}

	newDevicesMap := make(map[string]*Device, len(devices))
	for i := range devices {
		newDevicesMap[devices[i].UUID] = &devices[i]
	}

	m.mu.Lock()
	m.devices = newDevicesMap
	m.mu.Unlock()

	return devices, nil
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
