// Package ffmpeg provides functionality for managing and using FFmpeg binaries.
// It supports auto-downloading FFmpeg on Windows if not found in PATH.
package ffmpeg

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// FFmpeg download URL for Windows (gyan.dev essentials build - smaller, has what we need)
const (
	windowsFFmpegURL = "https://www.gyan.dev/ffmpeg/builds/ffmpeg-release-essentials.zip"
)

var (
	// cachedFFmpegPath stores the resolved path to ffmpeg binary
	cachedFFmpegPath string
	// cachedFFprobePath stores the resolved path to ffprobe binary
	cachedFFprobePath string
	// pathMutex protects the cached paths
	pathMutex sync.RWMutex
	// ErrFFmpegNotFound indicates ffmpeg is not available
	ErrFFmpegNotFound = errors.New("ffmpeg not found")
	// ErrUnsupportedPlatform indicates the platform doesn't support auto-download
	ErrUnsupportedPlatform = errors.New("auto-download not supported on this platform")
)

// Manager handles FFmpeg operations
type Manager struct {
	// BinDir is the directory where ffmpeg binaries are stored/downloaded
	BinDir string
}

// NewManager creates a new FFmpeg manager with binaries in the specified directory
func NewManager(binDir string) *Manager {
	return &Manager{BinDir: binDir}
}

// GetFFmpegPath returns the path to ffmpeg, downloading if necessary
func (m *Manager) GetFFmpegPath(ctx context.Context) (string, error) {
	pathMutex.RLock()
	if cachedFFmpegPath != "" {
		path := cachedFFmpegPath
		pathMutex.RUnlock()
		return path, nil
	}
	pathMutex.RUnlock()

	return m.findOrDownloadFFmpeg(ctx)
}

// GetFFprobePath returns the path to ffprobe, downloading if necessary
func (m *Manager) GetFFprobePath(ctx context.Context) (string, error) {
	pathMutex.RLock()
	if cachedFFprobePath != "" {
		path := cachedFFprobePath
		pathMutex.RUnlock()
		return path, nil
	}
	pathMutex.RUnlock()

	// Ensure ffmpeg is downloaded (ffprobe comes with it)
	_, err := m.findOrDownloadFFmpeg(ctx)
	if err != nil {
		return "", err
	}

	pathMutex.RLock()
	path := cachedFFprobePath
	pathMutex.RUnlock()
	return path, nil
}

// findOrDownloadFFmpeg locates ffmpeg or downloads it
func (m *Manager) findOrDownloadFFmpeg(ctx context.Context) (string, error) {
	pathMutex.Lock()
	defer pathMutex.Unlock()

	// Double-check after acquiring lock
	if cachedFFmpegPath != "" {
		return cachedFFmpegPath, nil
	}

	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}

	// Check in BinDir first
	localFFmpeg := filepath.Join(m.BinDir, "ffmpeg"+ext)
	localFFprobe := filepath.Join(m.BinDir, "ffprobe"+ext)
	if _, err := os.Stat(localFFmpeg); err == nil {
		cachedFFmpegPath = localFFmpeg
		if _, err := os.Stat(localFFprobe); err == nil {
			cachedFFprobePath = localFFprobe
		}
		return cachedFFmpegPath, nil
	}

	// Check in PATH
	if path, err := exec.LookPath("ffmpeg" + ext); err == nil {
		cachedFFmpegPath = path
		if probePath, err := exec.LookPath("ffprobe" + ext); err == nil {
			cachedFFprobePath = probePath
		}
		return cachedFFmpegPath, nil
	}

	// Not found, try to download
	if runtime.GOOS != "windows" {
		return "", fmt.Errorf("%w: please install ffmpeg manually (e.g., apt install ffmpeg)", ErrFFmpegNotFound)
	}

	// Download for Windows
	if err := m.downloadFFmpegWindows(ctx); err != nil {
		return "", fmt.Errorf("failed to download ffmpeg: %w", err)
	}

	cachedFFmpegPath = localFFmpeg
	cachedFFprobePath = localFFprobe
	return cachedFFmpegPath, nil
}

// downloadFFmpegWindows downloads and extracts FFmpeg for Windows
func (m *Manager) downloadFFmpegWindows(ctx context.Context) error {
	// Create bin directory
	if err := os.MkdirAll(m.BinDir, 0755); err != nil {
		return fmt.Errorf("failed to create bin directory: %w", err)
	}

	// Download to temp file
	zipPath := filepath.Join(m.BinDir, "ffmpeg-download.zip")
	defer os.Remove(zipPath) // Clean up zip after extraction

	req, err := http.NewRequestWithContext(ctx, "GET", windowsFFmpegURL, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download ffmpeg: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download ffmpeg: HTTP %d", resp.StatusCode)
	}

	out, err := os.Create(zipPath)
	if err != nil {
		return err
	}

	_, err = io.Copy(out, resp.Body)
	out.Close()
	if err != nil {
		return fmt.Errorf("failed to save ffmpeg download: %w", err)
	}

	// Extract the binaries we need
	return m.extractFFmpegFromZip(zipPath)
}

// extractFFmpegFromZip extracts ffmpeg.exe and ffprobe.exe from the downloaded zip
func (m *Manager) extractFFmpegFromZip(zipPath string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("failed to open zip: %w", err)
	}
	defer r.Close()

	binaries := map[string]string{
		"ffmpeg.exe":  filepath.Join(m.BinDir, "ffmpeg.exe"),
		"ffprobe.exe": filepath.Join(m.BinDir, "ffprobe.exe"),
	}

	// Debug: log all files in zip to find the binaries
	var foundFiles []string
	for _, f := range r.File {
		name := path.Base(f.Name)
		if name == "ffmpeg.exe" || name == "ffprobe.exe" {
			foundFiles = append(foundFiles, f.Name)
		}
	}
	fmt.Printf("[ffmpeg] Found binaries in zip: %v\n", foundFiles)

	extracted := 0
	for _, f := range r.File {
		// Use path.Base (not filepath.Base) because zip files always use forward slashes
		name := path.Base(f.Name)
		destPath, wanted := binaries[name]
		if !wanted {
			continue
		}

		// Skip directories
		if f.FileInfo().IsDir() {
			continue
		}

		fmt.Printf("[ffmpeg] Extracting %s to %s\n", f.Name, destPath)

		src, err := f.Open()
		if err != nil {
			return fmt.Errorf("failed to open %s in zip: %w", name, err)
		}

		dst, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
		if err != nil {
			src.Close()
			return fmt.Errorf("failed to create %s: %w", destPath, err)
		}

		written, err := io.Copy(dst, src)
		src.Close()
		dst.Close()
		if err != nil {
			return fmt.Errorf("failed to extract %s: %w", name, err)
		}
		fmt.Printf("[ffmpeg] Wrote %d bytes to %s\n", written, destPath)

		extracted++
		if extracted == len(binaries) {
			break
		}
	}

	if extracted < len(binaries) {
		return fmt.Errorf("could not find all required binaries in zip (found %d of %d)", extracted, len(binaries))
	}

	return nil
}

// ProbeResult contains the result of probing a media file
type ProbeResult struct {
	Streams []StreamInfo `json:"streams"`
	Format  FormatInfo   `json:"format"`
}

// StreamInfo contains information about a single stream
type StreamInfo struct {
	Index     int    `json:"index"`
	CodecName string `json:"codec_name"`
	CodecType string `json:"codec_type"` // "video", "audio", "subtitle"
	Channels  int    `json:"channels,omitempty"`
}

// FormatInfo contains format-level information
type FormatInfo struct {
	Filename   string `json:"filename"`
	FormatName string `json:"format_name"`
	Duration   string `json:"duration"`
}

// Probe runs ffprobe on the given file and returns information about its streams
func (m *Manager) Probe(ctx context.Context, filePath string) (*ProbeResult, error) {
	ffprobePath, err := m.GetFFprobePath(ctx)
	if err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, ffprobePath,
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		filePath,
	)

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe failed: %w", err)
	}

	var result ProbeResult
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("failed to parse ffprobe output: %w", err)
	}

	return &result, nil
}

// GetAudioCodec returns the codec of the first audio stream, or empty string if none
func (p *ProbeResult) GetAudioCodec() string {
	for _, s := range p.Streams {
		if s.CodecType == "audio" {
			return s.CodecName
		}
	}
	return ""
}

// NeedsTranscoding returns true if the audio codec is not browser-compatible
func (p *ProbeResult) NeedsTranscoding() bool {
	codec := strings.ToLower(p.GetAudioCodec())
	if codec == "" {
		return false // No audio, no transcoding needed
	}

	// Browser-compatible audio codecs in MP4/WebM containers
	compatible := map[string]bool{
		"aac":  true,
		"mp3":  true,
		"opus": true,
		"flac": true, // Some browsers support FLAC
	}

	return !compatible[codec]
}

// TranscodeAudio starts FFmpeg to transcode audio while copying video.
// Returns a reader for the transcoded output and a cleanup function.
func (m *Manager) TranscodeAudio(ctx context.Context, filePath string) (io.ReadCloser, error) {
	ffmpegPath, err := m.GetFFmpegPath(ctx)
	if err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, ffmpegPath,
		"-i", filePath,
		"-c:v", "copy",      // Copy video stream (no re-encoding)
		"-c:a", "aac",       // Transcode audio to AAC
		"-b:a", "192k",      // Audio bitrate
		"-movflags", "frag_keyframe+empty_moov+faststart", // Enable streaming
		"-f", "mp4",         // Output format
		"pipe:1",            // Output to stdout
	)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	// Capture stderr for debugging
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	// Return a wrapper that waits for the command to finish when closed
	return &transcodeReader{
		reader: stdout,
		cmd:    cmd,
	}, nil
}

// transcodeReader wraps the stdout pipe and ensures the command is cleaned up
type transcodeReader struct {
	reader io.ReadCloser
	cmd    *exec.Cmd
}

func (t *transcodeReader) Read(p []byte) (n int, err error) {
	return t.reader.Read(p)
}

func (t *transcodeReader) Close() error {
	t.reader.Close()
	// Kill the process if still running (e.g., client disconnected)
	if t.cmd.Process != nil {
		t.cmd.Process.Kill()
	}
	t.cmd.Wait()
	return nil
}

// IsAvailable checks if FFmpeg is available (either in PATH or can be downloaded)
func (m *Manager) IsAvailable(ctx context.Context) bool {
	_, err := m.GetFFmpegPath(ctx)
	return err == nil
}

// GenerateThumbnail creates a thumbnail image using FFmpeg.
// The thumbnail fits within a bounding box of the specified size while maintaining aspect ratio.
// Quality is 2-31 where 2 is best (for JPEG, maps to ~85% quality at value 2-5).
func (m *Manager) GenerateThumbnail(ctx context.Context, inputPath, outputPath string, size int, quality int) error {
	ffmpegPath, err := m.GetFFmpegPath(ctx)
	if err != nil {
		return err
	}

	// Scale filter: fit within bounding box, maintain aspect ratio, don't upscale
	// The expression scales the larger dimension to 'size' and calculates the other proportionally
	scaleFilter := fmt.Sprintf("scale='min(%d,iw)':'min(%d,ih)':force_original_aspect_ratio=decrease", size, size)

	cmd := exec.CommandContext(ctx, ffmpegPath,
		"-i", inputPath,
		"-vf", scaleFilter,
		"-qscale:v", fmt.Sprintf("%d", quality), // JPEG quality (2-5 is high quality)
		"-y", // Overwrite output
		outputPath,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg thumbnail failed: %w: %s", err, string(output))
	}

	return nil
}
