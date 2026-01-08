package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"jukel.org/q2/cast"
	"jukel.org/q2/db"
	"jukel.org/q2/ffmpeg"
	"jukel.org/q2/media"
	_ "jukel.org/q2/migrations"
	"jukel.org/q2/scanner"
)

const (
	q2Dir  = ".q2"
	dbFile = "q2.db"
)

// Metadata refresh progress state
var (
	metadataRefreshMu      sync.RWMutex
	metadataRefreshActive  bool
	metadataRefreshPath    string
	metadataRefreshCurrent string
	metadataRefreshTotal   int
	metadataRefreshDone    int
	metadataRefreshQueue   []string            // Queue of paths waiting to be refreshed
	metadataRefreshCancel  context.CancelFunc  // Function to cancel current scan
)

// MetadataRefreshRequest is the request body for metadata refresh.
type MetadataRefreshRequest struct {
	Path string `json:"path"`
}

// MetadataRefreshResponse is the response for metadata refresh start.
type MetadataRefreshResponse struct {
	Success       bool   `json:"success"`
	Message       string `json:"message"`
	QueuePosition int    `json:"queue_position,omitempty"` // Position in queue (0 = processing now)
}

// MetadataStatusResponse is the response for metadata refresh status.
type MetadataStatusResponse struct {
	Scanning     bool     `json:"scanning"`
	Path         string   `json:"path,omitempty"`
	CurrentFile  string   `json:"current_file,omitempty"`
	FilesTotal   int      `json:"files_total"`
	FilesDone    int      `json:"files_done"`
	Queue        []string `json:"queue,omitempty"`        // Paths waiting in queue
	QueueLength  int      `json:"queue_length"`           // Number of items in queue
}

// getMetadataRefreshStatus returns the current metadata refresh status.
func getMetadataRefreshStatus() MetadataStatusResponse {
	metadataRefreshMu.RLock()
	defer metadataRefreshMu.RUnlock()
	// Make a copy of the queue to avoid data races
	queueCopy := make([]string, len(metadataRefreshQueue))
	copy(queueCopy, metadataRefreshQueue)
	return MetadataStatusResponse{
		Scanning:    metadataRefreshActive,
		Path:        metadataRefreshPath,
		CurrentFile: metadataRefreshCurrent,
		FilesTotal:  metadataRefreshTotal,
		FilesDone:   metadataRefreshDone,
		Queue:       queueCopy,
		QueueLength: len(queueCopy),
	}
}

// getFolderIDForPath finds the folder_id for a given file path.
func getFolderIDForPath(database *db.DB, filePath string) (int64, error) {
	// Get all monitored folders
	rows, err := database.Query("SELECT id, path FROM folders ORDER BY LENGTH(path) DESC")
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	normalizedFilePath := normalizePath(filePath)
	for rows.Next() {
		var id int64
		var folderPath string
		if err := rows.Scan(&id, &folderPath); err != nil {
			continue
		}
		normalizedFolder := normalizePath(folderPath)
		// Check if file is within this folder
		prefix := normalizedFolder
		if !strings.HasSuffix(prefix, string(filepath.Separator)) {
			prefix += string(filepath.Separator)
		}
		if strings.HasPrefix(normalizedFilePath, prefix) || normalizedFilePath == normalizedFolder {
			return id, nil
		}
	}
	return 0, fmt.Errorf("no matching folder found for path: %s", filePath)
}

// upsertFile inserts or updates a file record and returns its ID.
func upsertFile(database *db.DB, folderID int64, filePath string, info os.FileInfo) (int64, error) {
	normalizedPath := normalizePath(filePath)
	filename := filepath.Base(filePath)
	ext := strings.ToLower(filepath.Ext(filePath))

	// Determine media type
	var mediaType string
	if isAudioFile(filePath) {
		mediaType = "audio"
	} else if isImageFile(filePath) {
		mediaType = "image"
	} else if isVideoFile(filePath) {
		mediaType = "video"
	}

	// Try to get existing file
	var existingID int64
	row := database.QueryRow("SELECT id FROM files WHERE path = ?", normalizedPath)
	if err := row.Scan(&existingID); err == nil {
		// File exists, update it
		database.Write(`
			UPDATE files SET
				filename = ?, extension = ?, mediatype = ?,
				size = ?, modified_at = ?, indexed_at = CURRENT_TIMESTAMP
			WHERE id = ?`,
			filename, ext, mediaType, info.Size(), info.ModTime(), existingID)
		return existingID, nil
	}

	// Insert new file
	result := database.Write(`
		INSERT INTO files (folder_id, path, filename, extension, mediatype, size, created_at, modified_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		folderID, normalizedPath, filename, ext, mediaType, info.Size(), info.ModTime(), info.ModTime())

	if result.Err != nil {
		return 0, result.Err
	}
	return result.LastInsertID, nil
}

// updateFileThumbnails updates the thumbnail paths for a file in the database.
func updateFileThumbnails(database *db.DB, fileID int64, smallPath, largePath string) {
	database.Write(`
		UPDATE files SET
			thumbnail_small_path = ?,
			thumbnail_large_path = ?
		WHERE id = ?`,
		smallPath, largePath, fileID)
}

// errScanCancelled is returned when a metadata scan is cancelled
var errScanCancelled = errors.New("scan cancelled")

// refreshMetadata walks a directory and extracts metadata for all audio/image/video files.
func refreshMetadata(database *db.DB, rootPath string, q2Dir string, ffmpegMgr *ffmpeg.Manager) {
	// Create cancellable context
	ctx, cancel := context.WithCancel(context.Background())

	// Set initial state
	metadataRefreshMu.Lock()
	metadataRefreshActive = true
	metadataRefreshPath = rootPath
	metadataRefreshCurrent = ""
	metadataRefreshTotal = 0
	metadataRefreshDone = 0
	metadataRefreshCancel = cancel
	metadataRefreshMu.Unlock()

	defer func() {
		// Clear cancel function and check queue
		metadataRefreshMu.Lock()
		metadataRefreshCancel = nil
		if len(metadataRefreshQueue) > 0 {
			// Pop the next path from the queue
			nextPath := metadataRefreshQueue[0]
			metadataRefreshQueue = metadataRefreshQueue[1:]
			metadataRefreshMu.Unlock()
			// Process next item (recursive call in same goroutine)
			refreshMetadata(database, nextPath, q2Dir, ffmpegMgr)
		} else {
			metadataRefreshActive = false
			metadataRefreshMu.Unlock()
		}
	}()

	// First pass: count files (can be cancelled)
	var totalFiles int
	filepath.WalkDir(rootPath, func(path string, d os.DirEntry, err error) error {
		// Check for cancellation
		select {
		case <-ctx.Done():
			return errScanCancelled
		default:
		}
		if err != nil || d.IsDir() {
			return nil
		}
		if isAudioFile(path) || isImageFile(path) || isVideoFile(path) {
			totalFiles++
		}
		return nil
	})

	// Check if cancelled during count
	select {
	case <-ctx.Done():
		return
	default:
	}

	metadataRefreshMu.Lock()
	metadataRefreshTotal = totalFiles
	metadataRefreshMu.Unlock()

	// Second pass: extract metadata
	filepath.WalkDir(rootPath, func(path string, d os.DirEntry, err error) error {
		// Check for cancellation
		select {
		case <-ctx.Done():
			return errScanCancelled
		default:
		}

		if err != nil || d.IsDir() {
			return nil
		}

		isAudio := isAudioFile(path)
		isImage := isImageFile(path)
		isVideo := isVideoFile(path)

		if !isAudio && !isImage && !isVideo {
			return nil
		}

		// Update current file
		metadataRefreshMu.Lock()
		metadataRefreshCurrent = path
		metadataRefreshMu.Unlock()

		// Get file info
		info, err := d.Info()
		if err != nil {
			metadataRefreshMu.Lock()
			metadataRefreshDone++
			metadataRefreshMu.Unlock()
			return nil
		}

		// Get folder ID for this file
		folderID, err := getFolderIDForPath(database, path)
		if err != nil {
			metadataRefreshMu.Lock()
			metadataRefreshDone++
			metadataRefreshMu.Unlock()
			return nil
		}

		// Upsert the file record
		fileID, err := upsertFile(database, folderID, path, info)
		if err != nil {
			metadataRefreshMu.Lock()
			metadataRefreshDone++
			metadataRefreshMu.Unlock()
			return nil
		}

		// Extract and save metadata
		if isAudio {
			if meta, err := media.ExtractAudioMetadata(path); err == nil {
				media.SaveAudioMetadata(database, fileID, meta)
			}
		} else if isImage {
			if meta, err := media.ExtractEXIF(path); err == nil {
				media.SaveImageMetadata(database, fileID, meta)
			}
			// Generate thumbnails for images
			if ffmpegMgr != nil {
				smallPath, largePath, err := media.GenerateBothThumbnails(ctx, path, q2Dir, ffmpegMgr)
				if err == nil {
					updateFileThumbnails(database, fileID, smallPath, largePath)
				}
			}
		} else if isVideo {
			// Generate thumbnails for videos
			if ffmpegMgr != nil {
				smallPath, largePath, err := media.GenerateBothVideoThumbnails(ctx, path, q2Dir, ffmpegMgr)
				if err == nil {
					updateFileThumbnails(database, fileID, smallPath, largePath)
				}
			}
		}

		metadataRefreshMu.Lock()
		metadataRefreshDone++
		metadataRefreshMu.Unlock()

		return nil
	})
}

// makeMetadataRefreshHandler creates a handler for POST /api/metadata/refresh.
func makeMetadataRefreshHandler(database *db.DB, q2Dir string, ffmpegMgr *ffmpeg.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		var req MetadataRefreshRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
			return
		}

		if req.Path == "" {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "path is required"})
			return
		}

		// Clean the path
		path, ok := cleanPath(req.Path)
		if !ok {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid path"})
			return
		}

		// Verify path is within monitored folders
		roots, err := getMonitoredFolders(database)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "database error"})
			return
		}

		if isPathWithinRoots(path, roots) == "" {
			writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "path not within monitored folders"})
			return
		}

		// Check if refresh is already in progress
		metadataRefreshMu.Lock()
		active := metadataRefreshActive
		currentPath := metadataRefreshPath

		// Check if this path is already being processed or in queue
		if active && currentPath == path {
			metadataRefreshMu.Unlock()
			writeJSON(w, http.StatusOK, MetadataRefreshResponse{
				Success:       true,
				Message:       "Already refreshing this folder",
				QueuePosition: 0,
			})
			return
		}

		// Check if path is already in queue
		for i, qPath := range metadataRefreshQueue {
			if qPath == path {
				metadataRefreshMu.Unlock()
				writeJSON(w, http.StatusOK, MetadataRefreshResponse{
					Success:       true,
					Message:       "Folder already in queue",
					QueuePosition: i + 1,
				})
				return
			}
		}

		if active {
			// Add to queue
			metadataRefreshQueue = append(metadataRefreshQueue, path)
			queuePos := len(metadataRefreshQueue)
			metadataRefreshMu.Unlock()

			writeJSON(w, http.StatusOK, MetadataRefreshResponse{
				Success:       true,
				Message:       fmt.Sprintf("Added to queue (position #%d)", queuePos),
				QueuePosition: queuePos,
			})
			return
		}
		metadataRefreshMu.Unlock()

		// Start refresh in background
		go refreshMetadata(database, path, q2Dir, ffmpegMgr)

		writeJSON(w, http.StatusOK, MetadataRefreshResponse{
			Success:       true,
			Message:       "Metadata refresh started",
			QueuePosition: 0,
		})
	}
}

// makeMetadataStatusHandler creates a handler for GET /api/metadata/status.
func makeMetadataStatusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		status := getMetadataRefreshStatus()
		writeJSON(w, http.StatusOK, status)
	}
}

// makeMetadataQueueRemoveHandler creates a handler for DELETE /api/metadata/queue.
func makeMetadataQueueRemoveHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		path := r.URL.Query().Get("path")
		if path == "" {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "path is required"})
			return
		}

		metadataRefreshMu.Lock()
		// Find and remove the path from queue
		found := false
		for i, qPath := range metadataRefreshQueue {
			if qPath == path {
				metadataRefreshQueue = append(metadataRefreshQueue[:i], metadataRefreshQueue[i+1:]...)
				found = true
				break
			}
		}
		metadataRefreshMu.Unlock()

		if !found {
			writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "path not in queue"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"message": "Removed from queue",
		})
	}
}

// makeMetadataQueuePrioritizeHandler creates a handler for POST /api/metadata/queue/prioritize.
func makeMetadataQueuePrioritizeHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		var req struct {
			Path string `json:"path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
			return
		}

		if req.Path == "" {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "path is required"})
			return
		}

		metadataRefreshMu.Lock()
		// Find the path in queue
		foundIdx := -1
		for i, qPath := range metadataRefreshQueue {
			if qPath == req.Path {
				foundIdx = i
				break
			}
		}

		if foundIdx == -1 {
			metadataRefreshMu.Unlock()
			writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "path not in queue"})
			return
		}

		// Move to front of queue
		if foundIdx > 0 {
			path := metadataRefreshQueue[foundIdx]
			metadataRefreshQueue = append(metadataRefreshQueue[:foundIdx], metadataRefreshQueue[foundIdx+1:]...)
			metadataRefreshQueue = append([]string{path}, metadataRefreshQueue...)
		}
		metadataRefreshMu.Unlock()

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"message": "Moved to top of queue",
		})
	}
}

// makeMetadataCancelHandler creates a handler for POST /api/metadata/cancel.
func makeMetadataCancelHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		metadataRefreshMu.Lock()
		if !metadataRefreshActive {
			metadataRefreshMu.Unlock()
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"success": false,
				"message": "No scan in progress",
			})
			return
		}

		if metadataRefreshCancel != nil {
			metadataRefreshCancel()
		}
		metadataRefreshMu.Unlock()

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"message": "Scan cancelled",
		})
	}
}

// homePageHTML is the HTML for the home page.
const homePageHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Q2</title>
    <style>
        * { box-sizing: border-box; }
        body { font-family: "Cascadia Code", "Fira Code", "JetBrains Mono", "SF Mono", Consolas, monospace; margin: 0; padding: 40px; background: #0d1117; color: #c9d1d9; }
        .container { max-width: 600px; margin: 0 auto; }
        h1 { color: #58a6ff; font-size: 48px; margin-bottom: 10px; }
        .subtitle { color: #8b949e; font-size: 16px; margin-bottom: 40px; }
        .nav-cards { display: flex; flex-direction: column; gap: 15px; }
        .nav-card { display: flex; align-items: center; gap: 20px; padding: 25px; background: #161b22; border: 1px solid #30363d; border-radius: 6px; text-decoration: none; color: inherit; transition: border-color 0.2s, background 0.2s; }
        .nav-card:hover { background: #1f2428; border-color: #58a6ff; }
        .nav-card .icon { font-size: 32px; }
        .nav-card .info h2 { margin: 0 0 5px 0; color: #58a6ff; font-size: 18px; }
        .nav-card .info p { margin: 0; color: #8b949e; font-size: 13px; }
    </style>
</head>
<body>
    <div class="container">
        <h1>&gt; Q2_</h1>
        <p class="subtitle">// media folder manager</p>
        <div class="nav-cards">
            <a href="/browse" class="nav-card">
                <span class="icon">📁</span>
                <div class="info">
                    <h2>./browse</h2>
                    <p>Navigate through monitored folders and view files</p>
                </div>
            </a>
            <a href="/schema" class="nav-card">
                <span class="icon">📊</span>
                <div class="info">
                    <h2>./schema</h2>
                    <p>View database tables, columns, and indexes</p>
                </div>
            </a>
        </div>
    </div>
</body>
</html>`

// homeEndpoint serves the home page with navigation links.
func homeEndpoint(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, homePageHTML)
}

// API response types for file browser

// RootFolder represents a monitored folder.
type RootFolder struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

// RootsResponse is the response for /api/roots.
type RootsResponse struct {
	Roots []RootFolder `json:"roots"`
}

// FileEntry represents a file or directory in a listing.
type FileEntry struct {
	Name     string `json:"name"`
	Type     string `json:"type"` // "file" or "dir"
	Size     int64  `json:"size"`
	Modified string `json:"modified"` // ISO 8601 format
	// Optional metadata fields (populated when ?metadata=true)
	MediaType      string `json:"mediaType,omitempty"`      // "image", "audio", "video", or empty
	ThumbnailSmall string `json:"thumbnailSmall,omitempty"` // URL to small thumbnail
	ThumbnailLarge string `json:"thumbnailLarge,omitempty"` // URL to large thumbnail
	// Audio-specific metadata
	Title    string `json:"title,omitempty"`
	Artist   string `json:"artist,omitempty"`
	Album    string `json:"album,omitempty"`
	Duration int    `json:"duration,omitempty"` // Duration in seconds
}

// BrowseResponse is the response for /api/browse.
type BrowseResponse struct {
	Path    string      `json:"path"`
	Parent  *string     `json:"parent"` // nil if this is a root folder
	Entries []FileEntry `json:"entries"`
}

// ErrorResponse is returned for API errors.
type ErrorResponse struct {
	Error string `json:"error"`
}

// getMonitoredFolders returns all monitored folder paths from the database.
func getMonitoredFolders(database *db.DB) ([]string, error) {
	rows, err := database.Query("SELECT path FROM folders ORDER BY path")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var folders []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		folders = append(folders, path)
	}
	return folders, rows.Err()
}

// isPathWithinRoots checks if the given path is within one of the monitored folders.
// Returns the matching root folder path if valid, or empty string if not.
func isPathWithinRoots(path string, roots []string) string {
	normalizedPath := normalizePath(path)
	for _, root := range roots {
		normalizedRoot := normalizePath(root)
		// Check if path equals root or is a subdirectory of root
		if normalizedPath == normalizedRoot {
			return root
		}
		// Ensure we're checking for a proper subdirectory (with path separator)
		// Handle drive roots (e.g., "P:\") which already end with a separator
		prefix := normalizedRoot
		if !strings.HasSuffix(prefix, string(filepath.Separator)) {
			prefix += string(filepath.Separator)
		}
		if strings.HasPrefix(normalizedPath, prefix) {
			return root
		}
	}
	return ""
}

// listDirectory returns the contents of a directory.
func listDirectory(path string) ([]FileEntry, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	result := make([]FileEntry, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue // Skip entries we can't read
		}

		entryType := "file"
		if entry.IsDir() {
			entryType = "dir"
		}

		result = append(result, FileEntry{
			Name:     entry.Name(),
			Type:     entryType,
			Size:     info.Size(),
			Modified: info.ModTime().UTC().Format(time.RFC3339),
		})
	}
	return result, nil
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// makeRootsHandler creates a handler for /api/roots.
func makeRootsHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		folders, err := getMonitoredFolders(database)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "database error"})
			return
		}

		roots := make([]RootFolder, len(folders))
		for i, path := range folders {
			roots[i] = RootFolder{
				Path: path,
				Name: filepath.Base(path),
			}
		}

		writeJSON(w, http.StatusOK, RootsResponse{Roots: roots})
	}
}

// audioContentTypes maps audio file extensions to their MIME types.
var audioContentTypes = map[string]string{
	".mp3":  "audio/mpeg",
	".wav":  "audio/wav",
	".flac": "audio/flac",
	".aac":  "audio/aac",
	".ogg":  "audio/ogg",
	".wma":  "audio/x-ms-wma",
	".m4a":  "audio/mp4",
}

// isAudioFile checks if the file extension is a supported audio format.
func isAudioFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	_, ok := audioContentTypes[ext]
	return ok
}

// imageContentTypes maps image file extensions to their MIME types.
var imageContentTypes = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".gif":  "image/gif",
	".webp": "image/webp",
	".bmp":  "image/bmp",
	".svg":  "image/svg+xml",
	".ico":  "image/x-icon",
}

// isImageFile checks if the file extension is a supported image format.
func isImageFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	_, ok := imageContentTypes[ext]
	return ok
}

// videoContentTypes maps video file extensions to their MIME types.
var videoContentTypes = map[string]string{
	".mp4":  "video/mp4",
	".webm": "video/webm",
	".ogv":  "video/ogg",
	".mov":  "video/quicktime",
	".avi":  "video/x-msvideo",
	".mkv":  "video/x-matroska",
	".m4v":  "video/mp4",
}

// isVideoFile checks if the file extension is a supported video format.
func isVideoFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	_, ok := videoContentTypes[ext]
	return ok
}

// isPlaylistFile checks if a file is an M3U8 playlist.
func isPlaylistFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".m3u8" || ext == ".m3u"
}

// parseM3U8 parses an M3U8 playlist file and returns the list of songs.
func parseM3U8(path string) ([]PlaylistSong, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var songs []PlaylistSong
	scanner := bufio.NewScanner(file)
	var currentTitle string
	var currentDuration int

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if strings.HasPrefix(line, "#EXTM3U") {
			continue // Header
		}

		if strings.HasPrefix(line, "#EXTINF:") {
			// Parse #EXTINF:duration,title
			info := strings.TrimPrefix(line, "#EXTINF:")
			parts := strings.SplitN(info, ",", 2)
			if len(parts) >= 1 {
				currentDuration, _ = strconv.Atoi(strings.TrimSpace(parts[0]))
			}
			if len(parts) >= 2 {
				currentTitle = strings.TrimSpace(parts[1])
			}
			continue
		}

		if strings.HasPrefix(line, "#") || line == "" {
			continue // Comment or empty
		}

		// This is a file path
		title := currentTitle
		if title == "" {
			title = filepath.Base(line)
		}
		songs = append(songs, PlaylistSong{
			Path:     line,
			Title:    title,
			Duration: currentDuration,
		})
		currentTitle = ""
		currentDuration = 0
	}

	return songs, scanner.Err()
}

// writeM3U8 writes a playlist to an M3U8 file.
func writeM3U8(path string, songs []PlaylistSong) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	// Write header
	fmt.Fprintln(file, "#EXTM3U")

	for _, song := range songs {
		// Write EXTINF line
		fmt.Fprintf(file, "#EXTINF:%d,%s\n", song.Duration, song.Title)
		// Write path
		fmt.Fprintln(file, song.Path)
	}

	return nil
}

// sanitizePlaylistName sanitizes a playlist name to be a valid filename.
func sanitizePlaylistName(name string) string {
	// Remove or replace invalid filename characters
	invalid := []string{"<", ">", ":", "\"", "/", "\\", "|", "?", "*"}
	result := name
	for _, char := range invalid {
		result = strings.ReplaceAll(result, char, "_")
	}
	// Trim spaces and dots from ends
	result = strings.Trim(result, " .")
	if result == "" {
		result = "Untitled"
	}
	return result
}

// ensurePlaylistsFolder creates the playlists directory and adds it as a monitored folder.
func ensurePlaylistsFolder(baseDir string, database *db.DB) (string, error) {
	playlistDir := filepath.Join(baseDir, "playlists")

	// Create directory if it doesn't exist
	if err := os.MkdirAll(playlistDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create playlists directory: %w", err)
	}

	// Get absolute path
	absPath, err := filepath.Abs(playlistDir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve playlists path: %w", err)
	}

	// Add as monitored folder (silently ignore if already exists)
	normalizedPath := normalizePath(absPath)
	database.Write(
		"INSERT OR IGNORE INTO folders (path) VALUES (?)",
		normalizedPath,
	)

	return absPath, nil
}

// makeStreamHandler creates a handler for /api/stream that serves audio files.
// Supports Range requests for seeking.
func makeStreamHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Handle CORS preflight for Chromecast
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Range")
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		path := r.URL.Query().Get("path")
		if path == "" {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "path parameter required"})
			return
		}

		// Log stream requests (helps debug Cast issues)
		fmt.Printf("Stream request from %s: %s (Range: %s)\n", r.RemoteAddr, path, r.Header.Get("Range"))

		// Clean the path
		path, ok := cleanPath(path)
		if !ok {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid path"})
			return
		}

		// Get monitored folders
		roots, err := getMonitoredFolders(database)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "database error"})
			return
		}

		// Verify path is within a monitored folder
		if isPathWithinRoots(path, roots) == "" {
			writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "path not within monitored folders"})
			return
		}

		// Check if file exists and is an audio file
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "file not found"})
			} else {
				writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "cannot access file"})
			}
			return
		}
		if info.IsDir() {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "path is a directory"})
			return
		}

		if !isAudioFile(path) {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "not an audio file"})
			return
		}

		// Get content type
		ext := strings.ToLower(filepath.Ext(path))
		contentType := audioContentTypes[ext]

		// Open the file
		file, err := os.Open(path)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "cannot open file"})
			return
		}
		defer file.Close()

		// Set content type and CORS headers (needed for Chromecast)
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Expose-Headers", "Content-Length, Content-Range")

		// Use http.ServeContent for Range request support
		http.ServeContent(w, r, filepath.Base(path), info.ModTime(), file)
	}
}

// makeImageHandler creates a handler for /api/image that serves image files.
func makeImageHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		path := r.URL.Query().Get("path")
		if path == "" {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "path parameter required"})
			return
		}

		// Clean the path
		path, ok := cleanPath(path)
		if !ok {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid path"})
			return
		}

		// Get monitored folders
		roots, err := getMonitoredFolders(database)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "database error"})
			return
		}

		// Verify path is within a monitored folder
		if isPathWithinRoots(path, roots) == "" {
			writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "path not within monitored folders"})
			return
		}

		// Check if file exists and is an image file
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "file not found"})
			} else {
				writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "cannot access file"})
			}
			return
		}
		if info.IsDir() {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "path is a directory"})
			return
		}

		if !isImageFile(path) {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "not an image file"})
			return
		}

		// Get content type
		ext := strings.ToLower(filepath.Ext(path))
		contentType := imageContentTypes[ext]

		// Open the file
		file, err := os.Open(path)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "cannot open file"})
			return
		}
		defer file.Close()

		// Set content type and CORS headers (needed for Chromecast)
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Expose-Headers", "Content-Length, Content-Range")

		// Use http.ServeContent for caching support
		http.ServeContent(w, r, filepath.Base(path), info.ModTime(), file)
	}
}

// makeThumbnailHandler creates a handler for /api/thumbnail that serves image thumbnails.
// Query params: path (original image path), size (small or large)
func makeThumbnailHandler(database *db.DB, q2Dir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		originalPath := r.URL.Query().Get("path")
		if originalPath == "" {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "path parameter required"})
			return
		}

		originalPath, ok := cleanPath(originalPath)
		if !ok {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid path"})
			return
		}

		// Verify path is within monitored folders
		roots, err := getMonitoredFolders(database)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "database error"})
			return
		}

		if isPathWithinRoots(originalPath, roots) == "" {
			writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "path not within monitored folders"})
			return
		}

		// Determine size
		sizeParam := r.URL.Query().Get("size")
		var size int
		switch sizeParam {
		case "large":
			size = media.LargeThumbnailSize
		default:
			size = media.SmallThumbnailSize
		}

		// Get the thumbnail path
		thumbRelPath := media.GetThumbnailPath(originalPath, size)
		thumbFullPath := filepath.Join(q2Dir, thumbRelPath)

		// Check if thumbnail exists
		info, err := os.Stat(thumbFullPath)
		if err != nil {
			if os.IsNotExist(err) {
				writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "thumbnail not found, run metadata refresh first"})
			} else {
				writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "cannot access thumbnail"})
			}
			return
		}

		file, err := os.Open(thumbFullPath)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "cannot open thumbnail"})
			return
		}
		defer file.Close()

		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "public, max-age=31536000") // Cache for 1 year
		http.ServeContent(w, r, filepath.Base(thumbFullPath), info.ModTime(), file)
	}
}

// makeVideoHandler creates a handler for /api/video that serves video files.
// Supports Range requests for seeking. Automatically transcodes incompatible audio codecs.
func makeVideoHandler(database *db.DB, ffmpegMgr *ffmpeg.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Handle CORS preflight for Chromecast
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Range")
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		path := r.URL.Query().Get("path")
		if path == "" {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "path parameter required"})
			return
		}

		path, ok := cleanPath(path)
		if !ok {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid path"})
			return
		}

		roots, err := getMonitoredFolders(database)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "database error"})
			return
		}

		if isPathWithinRoots(path, roots) == "" {
			writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "path not within monitored folders"})
			return
		}

		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "file not found"})
			} else {
				writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "cannot access file"})
			}
			return
		}
		if info.IsDir() {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "path is a directory"})
			return
		}

		if !isVideoFile(path) {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "not a video file"})
			return
		}

		// Check if transcoding is needed
		ctx := r.Context()
		needsTranscode := false
		if ffmpegMgr != nil {
			probe, err := ffmpegMgr.Probe(ctx, path)
			if err != nil {
				fmt.Printf("[video] Probe error (will serve directly): %v\n", err)
			} else if probe.NeedsTranscoding() {
				fmt.Printf("[video] Audio codec %q needs transcoding\n", probe.GetAudioCodec())
				needsTranscode = true
			} else {
				fmt.Printf("[video] Audio codec %q is browser-compatible\n", probe.GetAudioCodec())
			}
		}

		// Set CORS headers (needed for Chromecast)
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Expose-Headers", "Content-Length, Content-Range")

		if needsTranscode {
			// Transcode audio on the fly
			w.Header().Set("Content-Type", "video/mp4")
			// Cannot use Range requests with transcoding
			w.Header().Set("Accept-Ranges", "none")

			reader, err := ffmpegMgr.TranscodeAudio(ctx, path)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "transcoding failed: " + err.Error()})
				return
			}
			defer reader.Close()

			w.WriteHeader(http.StatusOK)
			io.Copy(w, reader)
			return
		}

		// Serve file directly (supports Range requests)
		ext := strings.ToLower(filepath.Ext(path))
		contentType := videoContentTypes[ext]
		w.Header().Set("Content-Type", contentType)

		file, err := os.Open(path)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "cannot open file"})
			return
		}
		defer file.Close()

		http.ServeContent(w, r, filepath.Base(path), info.ModTime(), file)
	}
}

// CastPlayRequest is the request body for /api/cast/play.
type CastPlayRequest struct {
	Path        string `json:"path"`
	ContentType string `json:"content_type"`
	Title       string `json:"title"`
}

// CastConnectRequest is the request body for /api/cast/connect.
type CastConnectRequest struct {
	UUID string `json:"uuid"`
}

// CastSeekRequest is the request body for /api/cast/seek.
type CastSeekRequest struct {
	Position float64 `json:"position"`
}

// CastVolumeRequest is the request body for /api/cast/volume.
type CastVolumeRequest struct {
	Level float64 `json:"level"`
	Muted *bool   `json:"muted,omitempty"`
}

// PlaylistSong represents a song in a playlist.
type PlaylistSong struct {
	Path     string `json:"path"`
	Title    string `json:"title"`
	Duration int    `json:"duration"` // seconds
}

// Playlist represents a playlist with metadata.
type Playlist struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Count int    `json:"count"`
}

// PlaylistWithContains adds a contains flag for checking if a song is in the playlist.
type PlaylistWithContains struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Contains bool   `json:"contains"`
}

// PlaylistResponse is the response for reading a playlist.
type PlaylistResponse struct {
	Name  string         `json:"name"`
	Path  string         `json:"path"`
	Songs []PlaylistSong `json:"songs"`
}

// PlaylistsResponse is the response for listing playlists.
type PlaylistsResponse struct {
	Playlists []Playlist `json:"playlists"`
}

// PlaylistCheckResponse is the response for checking which playlists contain a song.
type PlaylistCheckResponse struct {
	Playlists []PlaylistWithContains `json:"playlists"`
}

// PlaylistCreateRequest is the request body for creating a playlist.
type PlaylistCreateRequest struct {
	Name string `json:"name"`
}

// PlaylistAddRequest is the request body for adding a song to a playlist.
type PlaylistAddRequest struct {
	Playlist string `json:"playlist"`
	Song     string `json:"song"`
	Title    string `json:"title"`
	Duration int    `json:"duration"`
}

// PlaylistRemoveRequest is the request body for removing a song from a playlist.
type PlaylistRemoveRequest struct {
	Playlist string `json:"playlist"`
	Index    int    `json:"index"`
}

// PlaylistReorderRequest is the request body for reordering songs in a playlist.
type PlaylistReorderRequest struct {
	Playlist  string `json:"playlist"`
	FromIndex int    `json:"from_index"`
	ToIndex   int    `json:"to_index"`
}

// makeCastDevicesHandler creates a handler for /api/cast/devices.
// Supports ?type=audio to filter for audio-only devices, ?type=video for video devices.
func makeCastDevicesHandler(castMgr *cast.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		// Discover devices (10 second timeout for better discovery)
		ctx := r.Context()
		allDevices, err := castMgr.DiscoverDevices(ctx, 10*time.Second)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}

		// Filter by type if requested
		deviceType := r.URL.Query().Get("type")
		var devices []cast.Device
		for _, d := range allDevices {
			switch deviceType {
			case "audio":
				if d.IsAudio {
					devices = append(devices, d)
				}
			case "video":
				if !d.IsAudio {
					devices = append(devices, d)
				}
			default:
				devices = append(devices, d)
			}
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"devices": devices,
		})
	}
}

// makeCastConnectHandler creates a handler for /api/cast/connect.
func makeCastConnectHandler(castMgr *cast.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		var req CastConnectRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
			return
		}

		if req.UUID == "" {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "uuid required"})
			return
		}

		if err := castMgr.Connect(req.UUID); err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"status":  castMgr.GetStatus(),
		})
	}
}

// makeCastDisconnectHandler creates a handler for /api/cast/disconnect.
func makeCastDisconnectHandler(castMgr *cast.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		if err := castMgr.Disconnect(); err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
		})
	}
}

// makeCastPlayHandler creates a handler for /api/cast/play.
func makeCastPlayHandler(castMgr *cast.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		var req CastPlayRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
			return
		}

		if req.Path == "" {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "path required"})
			return
		}

		// Auto-detect content type if not provided
		if req.ContentType == "" {
			ext := strings.ToLower(filepath.Ext(req.Path))
			if ct, ok := audioContentTypes[ext]; ok {
				req.ContentType = ct
			} else if ct, ok := videoContentTypes[ext]; ok {
				req.ContentType = ct
			} else if ct, ok := imageContentTypes[ext]; ok {
				req.ContentType = ct
			}
		}

		mediaURL, err := castMgr.PlayMedia(req.Path, req.ContentType, req.Title)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success":   true,
			"media_url": mediaURL,
		})
	}
}

// makeCastPauseHandler creates a handler for /api/cast/pause.
func makeCastPauseHandler(castMgr *cast.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		if err := castMgr.Pause(); err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
		})
	}
}

// makeCastResumeHandler creates a handler for /api/cast/resume.
func makeCastResumeHandler(castMgr *cast.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		if err := castMgr.Resume(); err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
		})
	}
}

// makeCastStopHandler creates a handler for /api/cast/stop.
func makeCastStopHandler(castMgr *cast.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		if err := castMgr.Stop(); err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
		})
	}
}

// makeCastSeekHandler creates a handler for /api/cast/seek.
func makeCastSeekHandler(castMgr *cast.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		var req CastSeekRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
			return
		}

		if err := castMgr.Seek(req.Position); err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
		})
	}
}

// makeCastVolumeHandler creates a handler for /api/cast/volume.
func makeCastVolumeHandler(castMgr *cast.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		var req CastVolumeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
			return
		}

		if err := castMgr.SetVolume(req.Level); err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}

		if req.Muted != nil {
			if err := castMgr.SetMuted(*req.Muted); err != nil {
				writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
				return
			}
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
		})
	}
}

// makeCastStatusHandler creates a handler for /api/cast/status.
func makeCastStatusHandler(castMgr *cast.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		writeJSON(w, http.StatusOK, castMgr.GetStatus())
	}
}

// makePlaylistsHandler creates a handler for /api/playlists (list all playlists).
func makePlaylistsHandler(playlistDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		entries, err := os.ReadDir(playlistDir)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "failed to read playlists directory"})
			return
		}

		var playlists []Playlist
		for _, entry := range entries {
			if entry.IsDir() || !isPlaylistFile(entry.Name()) {
				continue
			}

			playlistPath := filepath.Join(playlistDir, entry.Name())
			songs, err := parseM3U8(playlistPath)
			count := 0
			if err == nil {
				count = len(songs)
			}

			// Get name from filename (without extension)
			name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))

			playlists = append(playlists, Playlist{
				Name:  name,
				Path:  playlistPath,
				Count: count,
			})
		}

		writeJSON(w, http.StatusOK, PlaylistsResponse{Playlists: playlists})
	}
}

// makePlaylistHandler creates a handler for /api/playlist (CRUD operations).
func makePlaylistHandler(playlistDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			// Read playlist contents
			path := r.URL.Query().Get("path")
			if path == "" {
				writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "path parameter required"})
				return
			}

			// Validate path is within playlists directory
			absPath, err := filepath.Abs(path)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid path"})
				return
			}
			absPlaylistDir, _ := filepath.Abs(playlistDir)
			if !strings.HasPrefix(strings.ToLower(absPath), strings.ToLower(absPlaylistDir)) {
				writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "path outside playlists directory"})
				return
			}

			songs, err := parseM3U8(path)
			if err != nil {
				writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "playlist not found"})
				return
			}

			name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			writeJSON(w, http.StatusOK, PlaylistResponse{
				Name:  name,
				Path:  path,
				Songs: songs,
			})

		case http.MethodPost:
			// Create new playlist
			var req PlaylistCreateRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
				return
			}

			if req.Name == "" {
				writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "name is required"})
				return
			}

			sanitizedName := sanitizePlaylistName(req.Name)
			playlistPath := filepath.Join(playlistDir, sanitizedName+".m3u8")

			// Check if already exists
			if _, err := os.Stat(playlistPath); err == nil {
				writeJSON(w, http.StatusConflict, ErrorResponse{Error: "playlist already exists"})
				return
			}

			// Create empty playlist
			if err := writeM3U8(playlistPath, []PlaylistSong{}); err != nil {
				writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "failed to create playlist"})
				return
			}

			writeJSON(w, http.StatusOK, map[string]interface{}{
				"success": true,
				"path":    playlistPath,
				"name":    sanitizedName,
			})

		case http.MethodDelete:
			// Delete playlist
			path := r.URL.Query().Get("path")
			if path == "" {
				writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "path parameter required"})
				return
			}

			// Validate path is within playlists directory
			absPath, err := filepath.Abs(path)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid path"})
				return
			}
			absPlaylistDir, _ := filepath.Abs(playlistDir)
			if !strings.HasPrefix(strings.ToLower(absPath), strings.ToLower(absPlaylistDir)) {
				writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "path outside playlists directory"})
				return
			}

			if err := os.Remove(path); err != nil {
				writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "playlist not found"})
				return
			}

			writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})

		default:
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
		}
	}
}

// makePlaylistAddHandler creates a handler for /api/playlist/add.
func makePlaylistAddHandler(playlistDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		var req PlaylistAddRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
			return
		}

		if req.Playlist == "" || req.Song == "" {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "playlist and song are required"})
			return
		}

		// Validate playlist path
		absPath, err := filepath.Abs(req.Playlist)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid playlist path"})
			return
		}
		absPlaylistDir, _ := filepath.Abs(playlistDir)
		if !strings.HasPrefix(strings.ToLower(absPath), strings.ToLower(absPlaylistDir)) {
			writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "path outside playlists directory"})
			return
		}

		// Read existing playlist
		songs, err := parseM3U8(req.Playlist)
		if err != nil {
			writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "playlist not found"})
			return
		}

		// Check if song already exists
		normalizedSong := normalizePath(req.Song)
		alreadyExists := false
		for _, song := range songs {
			if normalizePath(song.Path) == normalizedSong {
				alreadyExists = true
				break
			}
		}

		if alreadyExists {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"success":        true,
				"already_exists": true,
			})
			return
		}

		// Add song
		title := req.Title
		if title == "" {
			title = filepath.Base(req.Song)
		}
		songs = append(songs, PlaylistSong{
			Path:     req.Song,
			Title:    title,
			Duration: req.Duration,
		})

		// Write updated playlist
		if err := writeM3U8(req.Playlist, songs); err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "failed to update playlist"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success":        true,
			"already_exists": false,
		})
	}
}

// makePlaylistRemoveHandler creates a handler for /api/playlist/remove.
func makePlaylistRemoveHandler(playlistDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		var req PlaylistRemoveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
			return
		}

		if req.Playlist == "" {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "playlist is required"})
			return
		}

		// Validate playlist path
		absPath, err := filepath.Abs(req.Playlist)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid playlist path"})
			return
		}
		absPlaylistDir, _ := filepath.Abs(playlistDir)
		if !strings.HasPrefix(strings.ToLower(absPath), strings.ToLower(absPlaylistDir)) {
			writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "path outside playlists directory"})
			return
		}

		// Read existing playlist
		songs, err := parseM3U8(req.Playlist)
		if err != nil {
			writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "playlist not found"})
			return
		}

		// Validate index
		if req.Index < 0 || req.Index >= len(songs) {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid index"})
			return
		}

		// Remove song at index
		songs = append(songs[:req.Index], songs[req.Index+1:]...)

		// Write updated playlist
		if err := writeM3U8(req.Playlist, songs); err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "failed to update playlist"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
	}
}

// makePlaylistReorderHandler creates a handler for /api/playlist/reorder.
func makePlaylistReorderHandler(playlistDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		var req PlaylistReorderRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
			return
		}

		if req.Playlist == "" {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "playlist is required"})
			return
		}

		// Validate playlist path
		absPath, err := filepath.Abs(req.Playlist)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid playlist path"})
			return
		}
		absPlaylistDir, _ := filepath.Abs(playlistDir)
		if !strings.HasPrefix(strings.ToLower(absPath), strings.ToLower(absPlaylistDir)) {
			writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "path outside playlists directory"})
			return
		}

		// Read existing playlist
		songs, err := parseM3U8(req.Playlist)
		if err != nil {
			writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "playlist not found"})
			return
		}

		// Validate indices
		if req.FromIndex < 0 || req.FromIndex >= len(songs) ||
			req.ToIndex < 0 || req.ToIndex >= len(songs) {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid index"})
			return
		}

		// Reorder: remove from old position and insert at new position
		song := songs[req.FromIndex]
		songs = append(songs[:req.FromIndex], songs[req.FromIndex+1:]...)
		// Adjust toIndex if needed
		if req.ToIndex > req.FromIndex {
			req.ToIndex--
		}
		// Insert at new position
		songs = append(songs[:req.ToIndex+1], songs[req.ToIndex:]...)
		songs[req.ToIndex] = song

		// Write updated playlist
		if err := writeM3U8(req.Playlist, songs); err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "failed to update playlist"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
	}
}

// makePlaylistCheckHandler creates a handler for /api/playlist/check.
func makePlaylistCheckHandler(playlistDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		songPath := r.URL.Query().Get("song")
		if songPath == "" {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "song parameter required"})
			return
		}

		normalizedSong := normalizePath(songPath)

		entries, err := os.ReadDir(playlistDir)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "failed to read playlists directory"})
			return
		}

		var playlists []PlaylistWithContains
		for _, entry := range entries {
			if entry.IsDir() || !isPlaylistFile(entry.Name()) {
				continue
			}

			playlistPath := filepath.Join(playlistDir, entry.Name())
			songs, err := parseM3U8(playlistPath)
			if err != nil {
				continue
			}

			// Check if song is in this playlist
			contains := false
			for _, s := range songs {
				if normalizePath(s.Path) == normalizedSong {
					contains = true
					break
				}
			}

			name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
			playlists = append(playlists, PlaylistWithContains{
				Name:     name,
				Path:     playlistPath,
				Contains: contains,
			})
		}

		writeJSON(w, http.StatusOK, PlaylistCheckResponse{Playlists: playlists})
	}
}

// enrichEntriesWithMetadata adds metadata to file entries by querying the database.
func enrichEntriesWithMetadata(database *db.DB, q2Dir string, dirPath string, entries []FileEntry) {
	for i := range entries {
		entry := &entries[i]

		// Skip directories
		if entry.Type == "dir" {
			continue
		}

		// Determine media type based on extension
		fullPath := filepath.Join(dirPath, entry.Name)
		if isImageFile(fullPath) {
			entry.MediaType = "image"
		} else if isAudioFile(fullPath) {
			entry.MediaType = "audio"
		} else if isVideoFile(fullPath) {
			entry.MediaType = "video"
		}

		// Query database for file metadata
		normalizedPath := normalizePath(fullPath)
		row := database.QueryRow(`
			SELECT f.id, f.thumbnail_small_path, f.thumbnail_large_path,
			       am.title, am.artist, am.album, am.duration_seconds
			FROM files f
			LEFT JOIN audio_metadata am ON f.id = am.file_id
			WHERE f.path = ?`, normalizedPath)

		var fileID int64
		var thumbSmall, thumbLarge, title, artist, album *string
		var duration *int

		if err := row.Scan(&fileID, &thumbSmall, &thumbLarge, &title, &artist, &album, &duration); err == nil {
			// Set thumbnail URLs if available
			if thumbSmall != nil && *thumbSmall != "" {
				entry.ThumbnailSmall = "/api/thumbnail?path=" + url.QueryEscape(fullPath) + "&size=small"
			}
			if thumbLarge != nil && *thumbLarge != "" {
				entry.ThumbnailLarge = "/api/thumbnail?path=" + url.QueryEscape(fullPath) + "&size=large"
			}

			// Set audio metadata if available
			if title != nil {
				entry.Title = *title
			}
			if artist != nil {
				entry.Artist = *artist
			}
			if album != nil {
				entry.Album = *album
			}
			if duration != nil {
				entry.Duration = *duration
			}
		}
	}
}

// makeBrowseHandler creates a handler for /api/browse.
func makeBrowseHandler(database *db.DB, q2Dir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		path := r.URL.Query().Get("path")
		if path == "" {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "path parameter required"})
			return
		}

		// Check if metadata is requested
		includeMetadata := r.URL.Query().Get("metadata") == "true"

		// Clean the path
		path, ok := cleanPath(path)
		if !ok {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid path"})
			return
		}

		// Get monitored folders
		roots, err := getMonitoredFolders(database)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "database error"})
			return
		}

		// Verify path is within a monitored folder
		matchedRoot := isPathWithinRoots(path, roots)
		if matchedRoot == "" {
			writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "path not within monitored folders"})
			return
		}

		// Check if path exists and is a directory
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "path not found"})
			} else {
				writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "cannot access path"})
			}
			return
		}
		if !info.IsDir() {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "path is not a directory"})
			return
		}

		// List directory contents
		entries, err := listDirectory(path)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "cannot read directory"})
			return
		}

		// If metadata requested, enrich entries with database info
		if includeMetadata {
			enrichEntriesWithMetadata(database, q2Dir, path, entries)
		}

		// Determine parent path (nil if this is a root folder)
		var parent *string
		normalizedPath := normalizePath(path)
		normalizedRoot := normalizePath(matchedRoot)
		if normalizedPath != normalizedRoot {
			parentPath := filepath.Dir(path)
			parent = &parentPath
		}

		writeJSON(w, http.StatusOK, BrowseResponse{
			Path:    path,
			Parent:  parent,
			Entries: entries,
		})
	}
}

// browsePageHTML is the embedded HTML template for the file browser with Vue.js audio player.
const browsePageHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Q2 File Browser</title>
    <script src="https://unpkg.com/vue@3/dist/vue.global.js"></script>
    <style>
        * { box-sizing: border-box; }
        body { font-family: "Cascadia Code", "Fira Code", "JetBrains Mono", "SF Mono", Consolas, monospace; margin: 0; padding: 20px; padding-bottom: 100px; background: #0d1117; color: #c9d1d9; }

        /* Two-pane layout */
        .panes-wrapper { display: flex; gap: 20px; }
        .panes-wrapper.single-pane .pane { flex: 1; }
        .panes-wrapper.dual-pane .pane { flex: 1; min-width: 0; }
        .pane { background: #161b22; border-radius: 6px; border: 1px solid #30363d; overflow: hidden; display: flex; flex-direction: column; }
        .pane-content { flex: 1; overflow-y: auto; max-height: calc(100vh - 250px); }
        .pane table { width: 100%; }

        /* Header with toggle */
        .header-bar { display: flex; justify-content: space-between; align-items: center; margin-bottom: 20px; }
        .header-bar h1 { margin: 0; font-size: 22px; color: #58a6ff; }
        .view-toggle { background: #21262d; border: 1px solid #30363d; color: #c9d1d9; padding: 8px 16px; border-radius: 6px; cursor: pointer; font-family: inherit; font-size: 14px; display: flex; align-items: center; gap: 8px; }
        .view-toggle:hover { background: #30363d; border-color: #484f58; }
        .view-toggle.active { background: #1f6feb; border-color: #1f6feb; color: white; }

        .container { max-width: 1200px; margin: 0 auto; background: #161b22; border-radius: 6px; border: 1px solid #30363d; }
        .panes-wrapper.dual-pane { max-width: 100%; }
        h1 { margin: 0; padding: 20px; border-bottom: 1px solid #30363d; font-size: 22px; color: #58a6ff; }
        .breadcrumb { padding: 15px 20px; background: #0d1117; border-bottom: 1px solid #30363d; }
        .breadcrumb a { color: #58a6ff; text-decoration: none; cursor: pointer; }
        .breadcrumb a:hover { text-decoration: underline; }
        .breadcrumb span.sep { color: #484f58; margin: 0 8px; }
        table { width: 100%; border-collapse: collapse; }
        th, td { padding: 12px 20px; text-align: left; border-bottom: 1px solid #21262d; }
        th { background: #0d1117; cursor: pointer; user-select: none; font-weight: 600; color: #8b949e; }
        th:hover { background: #1f2428; }
        th .sort-indicator { margin-left: 5px; color: #484f58; }
        tr:hover { background: #1f2428; }
        .name-cell { display: flex; align-items: center; gap: 10px; }
        .icon { font-size: 18px; }
        .folder-link { color: #58a6ff; text-decoration: none; cursor: pointer; }
        .folder-link:hover { text-decoration: underline; }
        .file-name { color: #c9d1d9; }
        .size-cell, .modified-cell { color: #8b949e; }
        .type-cell { color: #6e7681; text-transform: uppercase; font-size: 11px; }
        .empty-message { padding: 40px; text-align: center; color: #8b949e; }
        .error-message { padding: 40px; text-align: center; color: #f85149; }
        .loading { padding: 40px; text-align: center; color: #8b949e; }
        .roots-list { padding: 20px; }
        .root-item { display: flex; align-items: center; gap: 10px; padding: 15px; border: 1px solid #30363d; border-radius: 6px; margin-bottom: 10px; cursor: pointer; background: #0d1117; }
        .root-item:hover { background: #1f2428; border-color: #58a6ff; }
        .root-item .icon { font-size: 24px; }
        .root-item .path { color: #8b949e; font-size: 13px; }
        .stats-bar { padding: 10px 20px; background: #0d1117; border-bottom: 1px solid #30363d; color: #8b949e; font-size: 13px; }
        .stats-bar .stat { margin-right: 20px; }
        .stats-bar .stat-value { font-weight: 600; color: #58a6ff; }

        /* Audio controls in file list */
        .audio-controls { display: flex; gap: 4px; margin-left: auto; }
        .audio-btn { background: none; border: 1px solid #30363d; border-radius: 4px; padding: 4px 8px; cursor: pointer; font-size: 11px; transition: all 0.2s; color: #8b949e; }
        .audio-btn:hover { background: #238636; color: white; border-color: #238636; }
        .audio-btn.play { background: #238636; color: white; border-color: #238636; }
        .audio-btn.play:hover { background: #2ea043; }

        /* Audio Player */
        .audio-player { position: fixed; bottom: 0; left: 0; right: 0; background: #1a1a2e; color: white; padding: 12px 20px; display: flex; align-items: center; gap: 15px; z-index: 1000; box-shadow: 0 -2px 10px rgba(0,0,0,0.3); }
        .audio-player.hidden { display: none; }
        .player-controls { display: flex; align-items: center; gap: 8px; }
        .player-btn { background: none; border: none; color: white; font-size: 20px; cursor: pointer; padding: 8px; border-radius: 50%; transition: background 0.2s; }
        .player-btn:hover { background: rgba(255,255,255,0.1); }
        .player-btn.play-pause { font-size: 28px; background: #0066cc; }
        .player-btn.play-pause:hover { background: #0052a3; }
        .track-info { flex: 1; min-width: 0; }
        .track-name { font-weight: 500; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
        .track-artist { font-size: 12px; color: #aaa; }
        .progress-container { flex: 2; display: flex; align-items: center; gap: 10px; }
        .progress-bar { flex: 1; height: 6px; background: #333; border-radius: 3px; cursor: pointer; position: relative; }
        .progress-fill { height: 100%; background: #0066cc; border-radius: 3px; transition: width 0.1s; }
        .time-display { font-size: 12px; color: #aaa; min-width: 90px; text-align: center; }
        .player-right { display: flex; align-items: center; gap: 10px; }
        .crossfade-toggle { display: flex; align-items: center; gap: 5px; font-size: 12px; color: #aaa; cursor: pointer; }
        .crossfade-toggle input { cursor: pointer; }
        .crossfade-toggle.active { color: #0066cc; }
        .queue-btn { position: relative; }
        .queue-count { position: absolute; top: -5px; right: -5px; background: #0066cc; color: white; font-size: 10px; padding: 2px 6px; border-radius: 10px; }

        /* Queue Panel */
        .queue-panel { position: fixed; bottom: 70px; right: 20px; width: 350px; max-height: 400px; background: #161b22; border: 1px solid #30363d; border-radius: 6px; box-shadow: 0 4px 20px rgba(0,0,0,0.4); z-index: 999; overflow: hidden; }
        .queue-panel.hidden { display: none; }
        .queue-header { padding: 15px; background: #0d1117; border-bottom: 1px solid #30363d; display: flex; justify-content: space-between; align-items: center; }
        .queue-header h3 { margin: 0; font-size: 14px; color: #c9d1d9; }
        .queue-clear { background: none; border: none; color: #f85149; cursor: pointer; font-size: 12px; }
        .queue-clear:hover { text-decoration: underline; }
        .queue-list { max-height: 320px; overflow-y: auto; }
        .queue-item { display: flex; align-items: center; padding: 10px 15px; border-bottom: 1px solid #21262d; gap: 10px; color: #c9d1d9; }
        .queue-item:hover { background: #1f2428; }
        .queue-item.playing { background: #1f6feb22; }
        .queue-item .num { color: #6e7681; font-size: 11px; width: 20px; }
        .queue-item .name { flex: 1; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; font-size: 13px; }
        .queue-item .remove { background: none; border: none; color: #6e7681; cursor: pointer; font-size: 16px; }
        .queue-item .remove:hover { color: #f85149; }
        .queue-item .move-btns { display: flex; flex-direction: column; gap: 2px; }
        .queue-item .move-btn { background: none; border: none; color: #6e7681; cursor: pointer; font-size: 10px; padding: 0; line-height: 1; }
        .queue-item .move-btn:hover { color: #58a6ff; }
        .queue-empty { padding: 30px; text-align: center; color: #6e7681; }

        /* Cast Button and Panel */
        .cast-btn { position: relative; }
        .cast-btn.casting { color: #58a6ff; }
        .cast-panel { position: fixed; bottom: 70px; right: 100px; width: 280px; background: #161b22; border: 1px solid #30363d; border-radius: 6px; box-shadow: 0 4px 20px rgba(0,0,0,0.4); z-index: 999; overflow: hidden; }
        .cast-panel.hidden { display: none; }
        .cast-header { padding: 15px; background: #0d1117; border-bottom: 1px solid #30363d; display: flex; justify-content: space-between; align-items: center; }
        .cast-header h3 { margin: 0; font-size: 14px; color: #c9d1d9; }
        .cast-refresh { background: none; border: 1px solid #30363d; color: #58a6ff; padding: 4px 10px; border-radius: 4px; cursor: pointer; font-size: 12px; }
        .cast-refresh:hover { background: #30363d; }
        .cast-scanning { font-size: 12px; color: #8b949e; }
        .cast-list { max-height: 300px; overflow-y: auto; }
        .cast-device { display: flex; align-items: center; padding: 12px 15px; border-bottom: 1px solid #21262d; gap: 10px; color: #c9d1d9; cursor: pointer; }
        .cast-device:hover { background: #1f2428; }
        .cast-device.active { background: #1f6feb22; color: #58a6ff; }
        .cast-device .icon { font-size: 18px; }
        .cast-device .name { flex: 1; font-size: 13px; }
        .cast-device .status { font-size: 11px; color: #6e7681; }
        .cast-searching { padding: 20px; text-align: center; color: #6e7681; }
        .cast-unavailable { padding: 20px; text-align: center; color: #6e7681; font-size: 13px; }

        /* Image Viewer */
        .image-viewer { position: fixed; top: 0; left: 0; right: 0; bottom: 80px; background: rgba(0,0,0,0.95); z-index: 998; display: flex; flex-direction: column; }
        .image-viewer.hidden { display: none; }
        .image-viewer-header { display: flex; justify-content: space-between; align-items: center; padding: 15px 20px; background: #161b22; border-bottom: 1px solid #30363d; }
        .image-viewer-title { color: #c9d1d9; font-size: 14px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
        .image-viewer-close { background: none; border: 1px solid #30363d; color: #c9d1d9; padding: 8px 16px; border-radius: 6px; cursor: pointer; font-size: 14px; }
        .image-viewer-close:hover { background: #30363d; }
        .image-viewer-content { flex: 1; display: flex; align-items: center; justify-content: center; padding: 20px; overflow: hidden; }
        .image-viewer-content img { max-width: 100%; max-height: 100%; object-fit: contain; }
        .image-viewer-nav { position: absolute; top: 50%; transform: translateY(-50%); background: rgba(0,0,0,0.7); border: 1px solid #30363d; color: #c9d1d9; padding: 20px 15px; cursor: pointer; font-size: 24px; }
        .image-viewer-nav:hover { background: rgba(88,166,255,0.3); }
        .image-viewer-nav.prev { left: 10px; }
        .image-viewer-nav.next { right: 10px; }
        .image-viewer-nav:disabled { opacity: 0.3; cursor: not-allowed; }
        .image-viewer-nav:disabled:hover { background: rgba(0,0,0,0.7); }
        .file-name.image-file { color: #58a6ff; cursor: pointer; }
        .file-name.image-file:hover { text-decoration: underline; }

        /* Video Player */
        .video-player { position: fixed; top: 0; left: 0; right: 0; bottom: 80px; background: rgba(0,0,0,0.98); z-index: 998; display: flex; flex-direction: column; }
        .video-player.hidden { display: none; }
        .video-player-header { display: flex; justify-content: space-between; align-items: center; padding: 15px 20px; background: #161b22; border-bottom: 1px solid #30363d; }
        .video-player-title { color: #c9d1d9; font-size: 14px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
        .video-player-close { background: none; border: 1px solid #30363d; color: #c9d1d9; padding: 8px 16px; border-radius: 6px; cursor: pointer; font-size: 14px; }
        .video-player-close:hover { background: #30363d; }
        .video-player-content { flex: 1; display: flex; align-items: center; justify-content: center; padding: 20px; overflow: hidden; background: #000; }
        .video-player-content video { max-width: 100%; max-height: 100%; }
        .video-controls { display: flex; align-items: center; gap: 15px; padding: 15px 20px; background: #161b22; border-top: 1px solid #30363d; }
        .video-btn { background: none; border: none; color: #c9d1d9; font-size: 24px; cursor: pointer; padding: 8px; border-radius: 4px; }
        .video-btn:hover { background: #30363d; }
        .video-btn.play-pause { font-size: 28px; background: #238636; color: white; }
        .video-btn.play-pause:hover { background: #2ea043; }
        .video-progress-container { flex: 1; display: flex; align-items: center; gap: 10px; }
        .video-progress-bar { flex: 1; height: 8px; background: #30363d; border-radius: 4px; cursor: pointer; position: relative; }
        .video-progress-fill { height: 100%; background: #238636; border-radius: 4px; transition: width 0.1s; }
        .video-time { font-size: 13px; color: #8b949e; min-width: 100px; text-align: center; }
        .file-name.video-file { color: #f0883e; cursor: pointer; }
        .file-name.video-file:hover { text-decoration: underline; }
        .video-btn.casting { color: #58a6ff; }
        .video-btn:disabled { opacity: 0.3; cursor: not-allowed; }
        .video-casting-indicator { display: flex; align-items: center; justify-content: center; gap: 15px; padding: 10px 20px; background: #1f6feb33; border-top: 1px solid #1f6feb; color: #58a6ff; font-size: 14px; }
        .stop-casting-btn { background: #30363d; border: 1px solid #484f58; color: #c9d1d9; padding: 5px 12px; border-radius: 4px; cursor: pointer; font-size: 13px; }
        .stop-casting-btn:hover { background: #484f58; }

        /* Playlist Popup */
        .playlist-popup { position: fixed; background: #161b22; border: 1px solid #30363d; border-radius: 6px; box-shadow: 0 4px 20px rgba(0,0,0,0.4); z-index: 1001; min-width: 220px; max-width: 300px; }
        .playlist-popup.hidden { display: none; }
        .playlist-popup-header { display: flex; justify-content: space-between; align-items: center; padding: 10px 15px; border-bottom: 1px solid #30363d; }
        .playlist-popup-header span { color: #c9d1d9; font-size: 13px; font-weight: 600; }
        .playlist-popup-header button { background: none; border: none; color: #6e7681; cursor: pointer; font-size: 18px; padding: 0; line-height: 1; }
        .playlist-popup-header button:hover { color: #c9d1d9; }
        .playlist-popup-list { max-height: 250px; overflow-y: auto; }
        .playlist-popup-item { padding: 10px 15px; cursor: pointer; color: #c9d1d9; display: flex; justify-content: space-between; align-items: center; font-size: 13px; }
        .playlist-popup-item:hover { background: #1f2428; }
        .already-here { color: #8b949e; font-size: 11px; margin-left: 8px; }
        .playlist-popup-empty { padding: 15px; text-align: center; color: #6e7681; font-size: 13px; }
        .playlist-popup-new { padding: 10px 15px; cursor: pointer; color: #58a6ff; border-top: 1px solid #30363d; font-size: 13px; }
        .playlist-popup-new:hover { background: #1f2428; }

        /* Playlist Viewer */
        .playlist-viewer { position: fixed; top: 0; left: 0; right: 0; bottom: 80px; background: rgba(0,0,0,0.9); z-index: 998; display: flex; align-items: center; justify-content: center; }
        .playlist-viewer.hidden { display: none; }
        .playlist-viewer-content { background: #161b22; border: 1px solid #30363d; border-radius: 8px; width: 90%; max-width: 600px; max-height: 80%; display: flex; flex-direction: column; }
        .playlist-viewer-header { display: flex; justify-content: space-between; align-items: center; padding: 15px 20px; border-bottom: 1px solid #30363d; }
        .playlist-viewer-header h2 { margin: 0; color: #c9d1d9; font-size: 18px; }
        .playlist-viewer-header .close-btn { background: none; border: 1px solid #30363d; color: #c9d1d9; width: 32px; height: 32px; border-radius: 6px; cursor: pointer; font-size: 18px; }
        .playlist-viewer-header .close-btn:hover { background: #30363d; }
        .playlist-viewer-actions { display: flex; gap: 10px; padding: 15px 20px; border-bottom: 1px solid #30363d; }
        .playlist-viewer-actions button { background: #238636; border: none; color: white; padding: 8px 16px; border-radius: 6px; cursor: pointer; font-size: 13px; }
        .playlist-viewer-actions button:hover { background: #2ea043; }
        .playlist-viewer-actions button.danger { background: #30363d; }
        .playlist-viewer-actions button.danger:hover { background: #da3633; }
        .playlist-viewer-list { flex: 1; overflow-y: auto; padding: 10px 0; }
        .playlist-song { display: flex; align-items: center; gap: 10px; padding: 8px 20px; }
        .playlist-song:hover { background: #1f2428; }
        .playlist-song .song-num { color: #6e7681; font-size: 12px; min-width: 25px; text-align: right; }
        .playlist-song .song-title { flex: 1; color: #c9d1d9; font-size: 13px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
        .playlist-song .song-controls { display: flex; gap: 4px; }
        .playlist-song .song-controls button { background: none; border: 1px solid #30363d; color: #8b949e; width: 28px; height: 28px; border-radius: 4px; cursor: pointer; font-size: 12px; }
        .playlist-song .song-controls button:hover { background: #238636; color: white; border-color: #238636; }
        .playlist-song .song-controls button:disabled { opacity: 0.3; cursor: not-allowed; }
        .playlist-song .song-controls button:disabled:hover { background: none; color: #8b949e; border-color: #30363d; }
        .playlist-song .song-controls .remove-btn:hover { background: #da3633; border-color: #da3633; }
        .playlist-empty { padding: 40px 20px; text-align: center; color: #6e7681; font-size: 14px; }
        .file-name.playlist-file { color: #a371f7; cursor: pointer; }
        .file-name.playlist-file:hover { text-decoration: underline; }
        /* Metadata Refresh */
        .header-actions { display: flex; gap: 10px; align-items: center; }
        .refresh-btn { background: #238636; border: 1px solid #238636; color: white; padding: 6px 12px; border-radius: 6px; cursor: pointer; font-size: 13px; font-family: inherit; display: flex; align-items: center; gap: 6px; }
        .refresh-btn:hover { background: #2ea043; border-color: #2ea043; }
        .refresh-btn:disabled { opacity: 0.6; cursor: not-allowed; }
        .refresh-btn .spinner { width: 14px; height: 14px; border: 2px solid transparent; border-top-color: white; border-radius: 50%; animation: spin 0.8s linear infinite; }
        @keyframes spin { to { transform: rotate(360deg); } }
        .metadata-progress { background: #161b22; border: 1px solid #30363d; border-radius: 6px; padding: 8px 12px; font-size: 12px; color: #8b949e; display: flex; align-items: center; gap: 10px; }
        .metadata-progress .progress-bar { width: 100px; height: 6px; background: #21262d; border-radius: 3px; overflow: hidden; }
        .metadata-progress .progress-bar .progress-fill { height: 100%; background: #238636; transition: width 0.3s; }
        .metadata-progress .progress-text { white-space: nowrap; }
        .metadata-progress .queue-info { color: #f0883e; margin-left: 5px; }
        .metadata-progress.clickable { cursor: pointer; }
        .metadata-progress.clickable:hover { background: #1f2428; border-color: #58a6ff; }

        /* Metadata Queue Panel */
        .metadata-queue-panel { position: fixed; top: 80px; right: 20px; width: 400px; max-height: 500px; background: #161b22; border: 1px solid #30363d; border-radius: 6px; box-shadow: 0 4px 20px rgba(0,0,0,0.4); z-index: 999; overflow: hidden; }
        .metadata-queue-panel.hidden { display: none; }
        .metadata-queue-header { padding: 15px; background: #0d1117; border-bottom: 1px solid #30363d; display: flex; justify-content: space-between; align-items: center; }
        .metadata-queue-header h3 { margin: 0; font-size: 14px; color: #c9d1d9; }
        .metadata-queue-close { background: none; border: none; color: #8b949e; cursor: pointer; font-size: 18px; padding: 0; }
        .metadata-queue-close:hover { color: #c9d1d9; }
        .metadata-queue-content { max-height: 420px; overflow-y: auto; }
        .metadata-queue-current { padding: 15px; background: #1f6feb22; border-bottom: 1px solid #30363d; }
        .metadata-queue-current .current-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 5px; }
        .metadata-queue-current .label { font-size: 11px; color: #58a6ff; text-transform: uppercase; }
        .metadata-queue-current .cancel-btn { background: #da3633; border: none; color: white; padding: 4px 10px; border-radius: 4px; cursor: pointer; font-size: 11px; }
        .metadata-queue-current .cancel-btn:hover { background: #f85149; }
        .metadata-queue-current .path { font-size: 13px; color: #c9d1d9; word-break: break-all; }
        .metadata-queue-current .progress { font-size: 12px; color: #8b949e; margin-top: 5px; }
        .metadata-queue-list { padding: 10px 0; }
        .metadata-queue-item { display: flex; align-items: center; padding: 10px 15px; border-bottom: 1px solid #21262d; gap: 10px; }
        .metadata-queue-item:hover { background: #1f2428; }
        .metadata-queue-item .num { color: #6e7681; font-size: 12px; min-width: 20px; }
        .metadata-queue-item .path { flex: 1; font-size: 13px; color: #c9d1d9; word-break: break-all; }
        .metadata-queue-item .actions { display: flex; gap: 5px; }
        .metadata-queue-item .action-btn { background: none; border: 1px solid #30363d; color: #8b949e; width: 28px; height: 28px; border-radius: 4px; cursor: pointer; font-size: 12px; display: flex; align-items: center; justify-content: center; }
        .metadata-queue-item .action-btn:hover { background: #30363d; color: #c9d1d9; }
        .metadata-queue-item .action-btn.priority:hover { background: #1f6feb; border-color: #1f6feb; color: white; }
        .metadata-queue-item .action-btn.remove:hover { background: #da3633; border-color: #da3633; color: white; }
        .metadata-queue-empty { padding: 30px; text-align: center; color: #6e7681; font-size: 13px; }

        /* Media View */
        .media-view { padding: 20px; }
        .media-section { margin-bottom: 30px; }
        .section-header { display: flex; align-items: center; gap: 10px; margin-bottom: 15px; padding-bottom: 10px; border-bottom: 1px solid #30363d; }
        .section-header h3 { margin: 0; color: #c9d1d9; font-size: 16px; font-weight: 600; }
        .section-header .count { color: #8b949e; font-size: 14px; }
        .thumbnail-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(140px, 1fr)); gap: 15px; }
        .thumb-item { display: flex; flex-direction: column; align-items: center; cursor: pointer; padding: 10px; border-radius: 6px; background: #0d1117; border: 1px solid #21262d; transition: all 0.2s; }
        .thumb-item:hover { background: #1f2428; border-color: #58a6ff; }
        .thumb-item img { width: 100%; aspect-ratio: 1; object-fit: cover; border-radius: 4px; background: #21262d; }
        .thumb-item .thumb-name { font-size: 12px; margin-top: 8px; text-align: center; color: #c9d1d9; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; width: 100%; }
        .thumb-item.video .thumb-name { color: #f0883e; }
        .audio-table { width: 100%; border-collapse: collapse; background: #0d1117; border-radius: 6px; overflow: hidden; }
        .audio-table th { background: #161b22; color: #8b949e; font-weight: 600; font-size: 12px; text-transform: uppercase; padding: 10px 15px; text-align: left; }
        .audio-table td { padding: 10px 15px; border-bottom: 1px solid #21262d; color: #c9d1d9; font-size: 13px; }
        .audio-table tr { cursor: pointer; transition: background 0.2s; }
        .audio-table tbody tr:hover { background: #1f2428; }
        .audio-table .title-col { color: #58a6ff; }
        .audio-table .duration-col { color: #8b949e; text-align: right; }
        .other-section table { width: 100%; }
    </style>
</head>
<body>
    <div id="app">
        <!-- Header with toggle -->
        <div class="header-bar">
            <h1>Q2 File Browser</h1>
            <div class="header-actions">
                <!-- Metadata Progress -->
                <div v-if="metadataStatus.scanning || metadataStatus.queue_length > 0"
                     class="metadata-progress clickable"
                     @click="toggleMetadataQueuePanel"
                     title="Click to manage queue">
                    <div class="progress-bar">
                        <div class="progress-fill" :style="{ width: metadataProgressPercent + '%' }"></div>
                    </div>
                    <span class="progress-text">
                        {{ metadataStatus.files_done }}/{{ metadataStatus.files_total }} files
                        <span v-if="metadataStatus.queue_length > 0" class="queue-info">(+{{ metadataStatus.queue_length }} queued)</span>
                    </span>
                </div>
                <!-- Refresh Metadata Button -->
                <button v-if="currentPath" class="refresh-btn" @click="refreshMetadata" :disabled="isCurrentPathQueued">
                    <span v-if="isCurrentPathScanning" class="spinner"></span>
                    {{ refreshButtonText }}
                </button>
                <button class="view-toggle" :class="{ active: dualPane }" @click="toggleDualPane">
                    {{ dualPane ? '◫ Dual Pane' : '▢ Single Pane' }}
                </button>
                <button class="view-toggle" :class="{ active: viewMode === 'media' }" @click="toggleViewMode">
                    {{ viewMode === 'media' ? '🎨 Media View' : '📋 File View' }}
                </button>
            </div>
        </div>

        <!-- Panes Wrapper -->
        <div class="panes-wrapper" :class="dualPane ? 'dual-pane' : 'single-pane'">
            <!-- Left Pane (Primary) -->
            <div class="pane">
                <!-- Breadcrumb -->
                <div class="breadcrumb">
                    <a @click="loadRoots">Roots</a>
                    <template v-if="currentPath">
                        <template v-for="(part, i) in pathParts" :key="i">
                            <span class="sep">/</span>
                            <a v-if="i < pathParts.length - 1" @click="browseTo(pathParts.slice(0, i + 1))">{{ part }}</a>
                            <strong v-else>{{ part }}</strong>
                        </template>
                    </template>
                </div>

                <!-- Stats Bar -->
                <div class="stats-bar" v-if="currentPath">
                    <span class="stat"><span class="stat-value">{{ folderCount }}</span> folder{{ folderCount !== 1 ? 's' : '' }}</span>
                    <span class="stat"><span class="stat-value">{{ fileCount }}</span> file{{ fileCount !== 1 ? 's' : '' }}</span>
                    <span class="stat"><span class="stat-value">{{ formatSize(totalSize) }}</span> total</span>
                </div>

                <!-- Content -->
                <div class="pane-content">
                    <div v-if="loading" class="loading">Loading...</div>
                    <div v-else-if="error" class="error-message">{{ error }}</div>

                    <!-- Roots List -->
                    <div v-else-if="!currentPath" class="roots-list">
                        <div v-if="roots.length === 0" class="empty-message">
                            No monitored folders. Use "q2 addfolder &lt;path&gt;" to add folders.
                        </div>
                        <div v-for="root in roots" :key="root.path" class="root-item" @click="browse(root.path)">
                            <span class="icon">📁</span>
                            <div>
                                <strong>{{ root.name }}</strong>
                                <div class="path">{{ root.path }}</div>
                            </div>
                        </div>
                    </div>

                    <!-- File Table (File View) -->
                    <table v-else-if="viewMode === 'file'">
                        <thead>
                            <tr>
                                <th @click="changeSort('name')">Name <span class="sort-indicator">{{ sortIndicator('name') }}</span></th>
                                <th @click="changeSort('type')">Type <span class="sort-indicator">{{ sortIndicator('type') }}</span></th>
                                <th @click="changeSort('size')">Size <span class="sort-indicator">{{ sortIndicator('size') }}</span></th>
                                <th @click="changeSort('modified')">Modified <span class="sort-indicator">{{ sortIndicator('modified') }}</span></th>
                            </tr>
                        </thead>
                        <tbody>
                            <tr v-if="sortedEntries.length === 0">
                                <td colspan="4" class="empty-message">This folder is empty</td>
                            </tr>
                            <tr v-for="entry in sortedEntries" :key="entry.name">
                                <td class="name-cell">
                                    <span class="icon">{{ entry.type === 'dir' ? '📁' : (isAudio(entry.name) ? '🎵' : (isImage(entry.name) ? '🖼️' : (isVideo(entry.name) ? '🎬' : (isPlaylist(entry.name) ? '📋' : '📄')))) }}</span>
                                    <a v-if="entry.type === 'dir'" class="folder-link" @click="browse(fullPath(entry.name))">{{ entry.name }}</a>
                                    <span v-else-if="isPlaylist(entry.name)" class="file-name playlist-file" @click="openPlaylist(entry)">{{ entry.name }}</span>
                                    <span v-else-if="isImage(entry.name)" class="file-name image-file" @click="openImage(entry)">{{ entry.name }}</span>
                                    <span v-else-if="isVideo(entry.name)" class="file-name video-file" @click="openVideo(entry)">{{ entry.name }}</span>
                                    <span v-else class="file-name">{{ entry.name }}</span>
                                    <div v-if="isAudio(entry.name)" class="audio-controls">
                                        <button class="audio-btn play" @click.stop="playNow(entry)" title="Play now">▶</button>
                                        <button class="audio-btn" @click.stop="addToQueueTop(entry)" title="Add to top of queue">⬆Q</button>
                                        <button class="audio-btn" @click.stop="addToQueueBottom(entry)" title="Add to bottom of queue">Q⬇</button>
                                        <button class="audio-btn" @click.stop="openPlaylistMenu($event, entry)" title="Add to playlist">...</button>
                                    </div>
                                </td>
                                <td class="type-cell">{{ entry.type === 'dir' ? 'Folder' : getExtension(entry.name) || 'File' }}</td>
                                <td class="size-cell">{{ entry.type === 'dir' ? '-' : formatSize(entry.size) }}</td>
                                <td class="modified-cell">{{ formatDate(entry.modified) }}</td>
                            </tr>
                        </tbody>
                    </table>

                    <!-- Media View -->
                    <div v-else-if="viewMode === 'media'" class="media-view">
                        <!-- Images Section -->
                        <div v-if="imageEntries.length" class="media-section">
                            <h3 class="section-header">Images ({{ imageEntries.length }})</h3>
                            <div class="thumbnail-grid">
                                <div v-for="img in imageEntries" :key="img.name" class="thumb-item" @click="openImage(img)">
                                    <img :src="img.thumbnailSmall || '/api/thumbnail?path=' + encodeURIComponent(fullPath(img.name)) + '&size=small'" :alt="img.name" loading="lazy" @error="handleThumbError">
                                    <span class="thumb-name">{{ img.name }}</span>
                                </div>
                            </div>
                        </div>

                        <!-- Audio Section -->
                        <div v-if="audioEntries.length" class="media-section">
                            <h3 class="section-header">Audio ({{ audioEntries.length }})</h3>
                            <table class="audio-table">
                                <thead>
                                    <tr>
                                        <th></th>
                                        <th>Title</th>
                                        <th>Artist</th>
                                        <th>Album</th>
                                        <th>Duration</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    <tr v-for="audio in audioEntries" :key="audio.name" class="audio-row" @dblclick="playNow(audio)">
                                        <td class="audio-actions">
                                            <button class="audio-btn play" @click.stop="playNow(audio)" title="Play">▶</button>
                                        </td>
                                        <td class="audio-title">{{ audio.title || audio.name }}</td>
                                        <td class="audio-artist">{{ audio.artist || '-' }}</td>
                                        <td class="audio-album">{{ audio.album || '-' }}</td>
                                        <td class="audio-duration">{{ audio.duration ? formatDuration(audio.duration) : '-' }}</td>
                                    </tr>
                                </tbody>
                            </table>
                        </div>

                        <!-- Videos Section -->
                        <div v-if="videoEntries.length" class="media-section">
                            <h3 class="section-header">Videos ({{ videoEntries.length }})</h3>
                            <div class="thumbnail-grid">
                                <div v-for="vid in videoEntries" :key="vid.name" class="thumb-item" @click="openVideo(vid)">
                                    <img :src="vid.thumbnailSmall || '/api/thumbnail?path=' + encodeURIComponent(fullPath(vid.name)) + '&size=small'" :alt="vid.name" loading="lazy" @error="handleThumbError">
                                    <span class="thumb-name">{{ vid.name }}</span>
                                </div>
                            </div>
                        </div>

                        <!-- Other Section (folders + misc files) -->
                        <div v-if="otherEntries.length" class="media-section">
                            <h3 class="section-header">Other ({{ otherEntries.length }})</h3>
                            <table>
                                <thead>
                                    <tr>
                                        <th>Name</th>
                                        <th>Type</th>
                                        <th>Size</th>
                                        <th>Modified</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    <tr v-for="entry in otherEntries" :key="entry.name">
                                        <td class="name-cell">
                                            <span class="icon">{{ entry.type === 'dir' ? '📁' : '📄' }}</span>
                                            <a v-if="entry.type === 'dir'" class="folder-link" @click="browse(fullPath(entry.name))">{{ entry.name }}</a>
                                            <span v-else class="file-name">{{ entry.name }}</span>
                                        </td>
                                        <td class="type-cell">{{ entry.type === 'dir' ? 'Folder' : getExtension(entry.name) || 'File' }}</td>
                                        <td class="size-cell">{{ entry.type === 'dir' ? '-' : formatSize(entry.size) }}</td>
                                        <td class="modified-cell">{{ formatDate(entry.modified) }}</td>
                                    </tr>
                                </tbody>
                            </table>
                        </div>

                        <div v-if="!imageEntries.length && !audioEntries.length && !videoEntries.length && !otherEntries.length" class="empty-message">
                            This folder is empty
                        </div>
                    </div>
                </div>
            </div>

            <!-- Right Pane (Secondary) - Only shown in dual pane mode -->
            <div class="pane" v-if="dualPane">
                <!-- Breadcrumb -->
                <div class="breadcrumb">
                    <a @click="loadRoots2">Roots</a>
                    <template v-if="pane2Path">
                        <template v-for="(part, i) in pane2PathParts" :key="i">
                            <span class="sep">/</span>
                            <a v-if="i < pane2PathParts.length - 1" @click="browseTo2(pane2PathParts.slice(0, i + 1))">{{ part }}</a>
                            <strong v-else>{{ part }}</strong>
                        </template>
                    </template>
                </div>

                <!-- Stats Bar -->
                <div class="stats-bar" v-if="pane2Path">
                    <span class="stat"><span class="stat-value">{{ pane2FolderCount }}</span> folder{{ pane2FolderCount !== 1 ? 's' : '' }}</span>
                    <span class="stat"><span class="stat-value">{{ pane2FileCount }}</span> file{{ pane2FileCount !== 1 ? 's' : '' }}</span>
                    <span class="stat"><span class="stat-value">{{ formatSize(pane2TotalSize) }}</span> total</span>
                </div>

                <!-- Content -->
                <div class="pane-content">
                    <div v-if="pane2Loading" class="loading">Loading...</div>
                    <div v-else-if="pane2Error" class="error-message">{{ pane2Error }}</div>

                    <!-- Roots List -->
                    <div v-else-if="!pane2Path" class="roots-list">
                        <div v-if="roots.length === 0" class="empty-message">
                            No monitored folders. Use "q2 addfolder &lt;path&gt;" to add folders.
                        </div>
                        <div v-for="root in roots" :key="root.path" class="root-item" @click="browse2(root.path)">
                            <span class="icon">📁</span>
                            <div>
                                <strong>{{ root.name }}</strong>
                                <div class="path">{{ root.path }}</div>
                            </div>
                        </div>
                    </div>

                    <!-- File Table -->
                    <table v-else>
                        <thead>
                            <tr>
                                <th @click="changeSort2('name')">Name <span class="sort-indicator">{{ sortIndicator2('name') }}</span></th>
                                <th @click="changeSort2('type')">Type <span class="sort-indicator">{{ sortIndicator2('type') }}</span></th>
                                <th @click="changeSort2('size')">Size <span class="sort-indicator">{{ sortIndicator2('size') }}</span></th>
                                <th @click="changeSort2('modified')">Modified <span class="sort-indicator">{{ sortIndicator2('modified') }}</span></th>
                            </tr>
                        </thead>
                        <tbody>
                            <tr v-if="pane2SortedEntries.length === 0">
                                <td colspan="4" class="empty-message">This folder is empty</td>
                            </tr>
                            <tr v-for="entry in pane2SortedEntries" :key="entry.name">
                                <td class="name-cell">
                                    <span class="icon">{{ entry.type === 'dir' ? '📁' : (isAudio(entry.name) ? '🎵' : (isImage(entry.name) ? '🖼️' : (isVideo(entry.name) ? '🎬' : (isPlaylist(entry.name) ? '📋' : '📄')))) }}</span>
                                    <a v-if="entry.type === 'dir'" class="folder-link" @click="browse2(pane2FullPath(entry.name))">{{ entry.name }}</a>
                                    <span v-else-if="isPlaylist(entry.name)" class="file-name playlist-file" @click="openPlaylist(entry)">{{ entry.name }}</span>
                                    <span v-else-if="isImage(entry.name)" class="file-name image-file" @click="openImage2(entry)">{{ entry.name }}</span>
                                    <span v-else-if="isVideo(entry.name)" class="file-name video-file" @click="openVideo2(entry)">{{ entry.name }}</span>
                                    <span v-else class="file-name">{{ entry.name }}</span>
                                    <div v-if="isAudio(entry.name)" class="audio-controls">
                                        <button class="audio-btn play" @click.stop="playNow2(entry)" title="Play now">▶</button>
                                        <button class="audio-btn" @click.stop="addToQueueTop2(entry)" title="Add to top of queue">⬆Q</button>
                                        <button class="audio-btn" @click.stop="addToQueueBottom2(entry)" title="Add to bottom of queue">Q⬇</button>
                                        <button class="audio-btn" @click.stop="openPlaylistMenu($event, entry, true)" title="Add to playlist">...</button>
                                    </div>
                                </td>
                                <td class="type-cell">{{ entry.type === 'dir' ? 'Folder' : getExtension(entry.name) || 'File' }}</td>
                                <td class="size-cell">{{ entry.type === 'dir' ? '-' : formatSize(entry.size) }}</td>
                                <td class="modified-cell">{{ formatDate(entry.modified) }}</td>
                            </tr>
                        </tbody>
                    </table>
                </div>
            </div>
        </div>

        <!-- Playlist Menu Popup -->
        <div class="playlist-popup" :class="{ hidden: !showPlaylistMenu }" :style="playlistMenuStyle">
            <div class="playlist-popup-header">
                <span>Add to Playlist</span>
                <button @click="closePlaylistMenu">×</button>
            </div>
            <div class="playlist-popup-list">
                <div v-for="pl in availablePlaylists" :key="pl.path"
                     class="playlist-popup-item"
                     @click="addToPlaylist(pl.path)">
                    {{ pl.name }}
                    <span v-if="pl.contains" class="already-here">(already here)</span>
                </div>
                <div v-if="availablePlaylists.length === 0" class="playlist-popup-empty">
                    No playlists yet
                </div>
                <div class="playlist-popup-new" @click="createNewPlaylist">
                    + Create new playlist...
                </div>
            </div>
        </div>

        <!-- Playlist Viewer Modal -->
        <div class="playlist-viewer" :class="{ hidden: !viewingPlaylist }">
            <div class="playlist-viewer-content">
                <div class="playlist-viewer-header">
                    <h2>{{ viewingPlaylist?.name }}</h2>
                    <button class="close-btn" @click="closePlaylistViewer">×</button>
                </div>
                <div class="playlist-viewer-actions">
                    <button @click="playAllFromPlaylist">▶ Play All</button>
                    <button @click="shuffleAndPlayPlaylist">🔀 Shuffle</button>
                    <button @click="deletePlaylist" class="danger">🗑 Delete Playlist</button>
                </div>
                <div class="playlist-viewer-list">
                    <div v-for="(song, i) in playlistSongs" :key="i" class="playlist-song">
                        <span class="song-num">{{ i + 1 }}</span>
                        <span class="song-title" :title="song.path">{{ song.title }}</span>
                        <div class="song-controls">
                            <button @click="playSongFromPlaylist(i)" title="Play">▶</button>
                            <button @click="movePlaylistSongUp(i)" :disabled="i === 0" title="Move up">▲</button>
                            <button @click="movePlaylistSongDown(i)" :disabled="i === playlistSongs.length - 1" title="Move down">▼</button>
                            <button @click="removeFromPlaylist(i)" title="Remove" class="remove-btn">×</button>
                        </div>
                    </div>
                    <div v-if="playlistSongs.length === 0" class="playlist-empty">
                        This playlist is empty. Add songs using the "..." button next to audio files.
                    </div>
                </div>
            </div>
        </div>

        <!-- Audio Player -->
        <div class="audio-player" :class="{ hidden: !currentTrack }">
            <div class="player-controls">
                <button class="player-btn" @click="playPrevious" title="Previous">⏮</button>
                <button class="player-btn play-pause" @click="togglePlay" :title="isPlaying ? 'Pause' : 'Play'">
                    {{ isPlaying ? '⏸' : '▶' }}
                </button>
                <button class="player-btn" @click="playNext" title="Next">⏭</button>
            </div>
            <div class="track-info">
                <div class="track-name">{{ currentTrack?.name || 'No track' }}</div>
            </div>
            <div class="progress-container">
                <span class="time-display">{{ formatTime(currentTime) }} / {{ formatTime(duration) }}</span>
                <div class="progress-bar" @click="seek($event)">
                    <div class="progress-fill" :style="{ width: progressPercent + '%' }"></div>
                </div>
            </div>
            <div class="player-right">
                <button class="player-btn" @click="toggleMute" :title="isMuted ? 'Unmute' : 'Mute'">
                    {{ isMuted ? '🔇' : '🔊' }}
                </button>
                <label class="crossfade-toggle" :class="{ active: crossfadeEnabled }">
                    <input type="checkbox" v-model="crossfadeEnabled"> Crossfade
                </label>
                <button class="player-btn queue-btn" @click="toggleQueue" title="Queue">
                    🎵
                    <span v-if="queue.length > 0" class="queue-count">{{ queue.length }}</span>
                </button>
                <button class="player-btn cast-btn" :class="{ casting: isCasting }" @click="toggleCastPanel" title="Cast">
                    📺
                </button>
            </div>
        </div>

        <!-- Metadata Queue Panel -->
        <div class="metadata-queue-panel" :class="{ hidden: !showMetadataQueuePanel }">
            <div class="metadata-queue-header">
                <h3>Metadata Refresh Queue</h3>
                <button class="metadata-queue-close" @click="showMetadataQueuePanel = false">×</button>
            </div>
            <div class="metadata-queue-content">
                <!-- Currently scanning -->
                <div v-if="metadataStatus.scanning" class="metadata-queue-current">
                    <div class="current-header">
                        <div class="label">Currently Scanning</div>
                        <button class="cancel-btn" @click="cancelMetadataScan" title="Cancel scan">Cancel</button>
                    </div>
                    <div class="path">{{ metadataStatus.path }}</div>
                    <div class="progress">{{ metadataStatus.files_done }}/{{ metadataStatus.files_total }} files ({{ metadataProgressPercent }}%)</div>
                </div>
                <!-- Queue list -->
                <div class="metadata-queue-list" v-if="metadataStatus.queue && metadataStatus.queue.length > 0">
                    <div v-for="(path, i) in metadataStatus.queue" :key="path" class="metadata-queue-item">
                        <span class="num">#{{ i + 1 }}</span>
                        <span class="path">{{ path }}</span>
                        <div class="actions">
                            <button v-if="i > 0" class="action-btn priority" @click="prioritizeInQueue(path)" title="Move to top">⬆</button>
                            <button class="action-btn remove" @click="removeFromMetadataQueue(path)" title="Remove">×</button>
                        </div>
                    </div>
                </div>
                <!-- Empty state -->
                <div v-if="!metadataStatus.scanning && (!metadataStatus.queue || metadataStatus.queue.length === 0)" class="metadata-queue-empty">
                    No folders in queue
                </div>
            </div>
        </div>

        <!-- Cast Panel -->
        <div class="cast-panel" :class="{ hidden: !showCastPanel }">
            <div class="cast-header">
                <h3>Cast to device</h3>
                <button v-if="!castScanning" class="cast-refresh" @click="scanCastDevices">Refresh</button>
                <span v-else class="cast-scanning">Scanning...</span>
            </div>
            <div class="cast-list">
                <div class="cast-device" :class="{ active: !isCasting }" @click="stopCasting">
                    <span class="icon">💻</span>
                    <span class="name">This device</span>
                    <span v-if="!isCasting" class="status">Playing</span>
                </div>
                <div v-if="isCasting" class="cast-device active">
                    <span class="icon">📺</span>
                    <span class="name">{{ castingTo }}</span>
                    <span class="status">Casting</span>
                </div>
                <div v-for="device in castDevices" :key="device.uuid"
                     class="cast-device"
                     :class="{ active: isCasting && castingTo === device.name }"
                     @click="connectCastDevice(device)">
                    <span class="icon">📺</span>
                    <span class="name">{{ device.name }}</span>
                    <span class="status">{{ device.device_type }}</span>
                </div>
                <div v-if="castDevices.length === 0 && !castScanning" class="cast-unavailable">
                    No devices found.<br>
                    <small>Click Refresh to scan</small>
                </div>
            </div>
        </div>

        <!-- Queue Panel -->
        <div class="queue-panel" :class="{ hidden: !showQueue }">
            <div class="queue-header">
                <h3>Queue ({{ queue.length }})</h3>
                <button class="queue-clear" @click="clearQueue" v-if="queue.length > 0">Clear all</button>
            </div>
            <div class="queue-list">
                <div v-if="queue.length === 0" class="queue-empty">Queue is empty</div>
                <div v-for="(track, i) in queue" :key="track.path + i"
                     class="queue-item" :class="{ playing: i === currentIndex }">
                    <span class="num">{{ i + 1 }}</span>
                    <span class="name" :title="track.name">{{ track.name }}</span>
                    <div class="move-btns">
                        <button class="move-btn" @click="moveUp(i)" v-if="i > 0" title="Move up">▲</button>
                        <button class="move-btn" @click="moveDown(i)" v-if="i < queue.length - 1" title="Move down">▼</button>
                    </div>
                    <button class="remove" @click="removeFromQueue(i)" title="Remove">×</button>
                </div>
            </div>
        </div>

        <!-- Image Viewer -->
        <div class="image-viewer" :class="{ hidden: !viewingImage }">
            <div class="image-viewer-header">
                <span class="image-viewer-title">{{ viewingImage?.name }}</span>
                <button class="image-viewer-close" @click="closeImage">Close (Esc)</button>
            </div>
            <div class="image-viewer-content">
                <img v-if="viewingImage" :src="'/api/image?path=' + encodeURIComponent(viewingImage._pane2Path ? (viewingImage._pane2Path + '\\\\' + viewingImage.name) : fullPath(viewingImage.name))" :alt="viewingImage.name">
            </div>
            <button class="image-viewer-nav prev" @click="prevImage" :disabled="!canPrevImage">❮</button>
            <button class="image-viewer-nav next" @click="nextImage" :disabled="!canNextImage">❯</button>
        </div>

        <!-- Video Player -->
        <div class="video-player" :class="{ hidden: !videoFile }">
            <div class="video-player-header">
                <span class="video-player-title">{{ videoFile?.name }}</span>
                <button class="video-player-close" @click="closeVideo">Close (Esc)</button>
            </div>
            <div class="video-player-content">
                <video v-if="videoFile"
                       ref="videoRef"
                       :src="'/api/video?path=' + encodeURIComponent(videoFile._pane2Path ? (videoFile._pane2Path + '\\\\' + videoFile.name) : fullPath(videoFile.name))"
                       @timeupdate="onVideoTimeUpdate"
                       @loadedmetadata="onVideoMetadata"
                       @play="onVideoPlay"
                       @pause="onVideoPause"
                       @ended="onVideoEnded">
                </video>
            </div>
            <div class="video-controls">
                <button class="video-btn play-pause" @click="toggleVideoPlay" :title="videoPlaying ? 'Pause' : 'Play'">
                    {{ videoPlaying ? '⏸' : '▶' }}
                </button>
                <div class="video-progress-container">
                    <span class="video-time">{{ formatTime(videoCurrentTime) }}</span>
                    <div class="video-progress-bar" @click="seekVideo($event)">
                        <div class="video-progress-fill" :style="{ width: videoProgressPercent + '%' }"></div>
                    </div>
                    <span class="video-time">{{ formatTime(videoDuration) }}</span>
                </div>
                <button class="video-btn" @click="toggleVideoMute" :title="videoMuted ? 'Unmute' : 'Mute'">
                    {{ videoMuted ? '🔇' : '🔊' }}
                </button>
                <button class="video-btn" :class="{ casting: isVideoCasting }" @click="openVideoCastPicker" :disabled="!castAvailable" title="Cast">
                    📺
                </button>
            </div>
            <div v-if="isVideoCasting" class="video-casting-indicator">
                Casting to {{ videoCastingTo }}
                <button class="stop-casting-btn" @click="stopVideoCasting">Stop</button>
            </div>
        </div>

        <!-- Hidden audio elements for crossfade -->
        <audio ref="audioA" @timeupdate="onTimeUpdate" @ended="onEnded" @loadedmetadata="onMetadata"></audio>
        <audio ref="audioB" @timeupdate="onTimeUpdate" @ended="onEnded" @loadedmetadata="onMetadata"></audio>
    </div>

    <script>
        const AUDIO_EXTENSIONS = ['mp3', 'wav', 'flac', 'aac', 'ogg', 'wma', 'm4a'];
        const IMAGE_EXTENSIONS = ['jpg', 'jpeg', 'png', 'gif', 'webp', 'bmp', 'svg', 'ico'];
        const VIDEO_EXTENSIONS = ['mp4', 'webm', 'ogv', 'mov', 'avi', 'mkv', 'm4v'];
        const CROSSFADE_DURATION = 3; // seconds

        const { createApp, ref, computed, watch, onMounted, nextTick } = Vue;

        createApp({
            setup() {
                // File browser state
                const roots = ref([]);
                const currentPath = ref(null);
                const entries = ref([]);
                const loading = ref(true);
                const error = ref(null);
                const sortColumn = ref('name');
                const sortAsc = ref(true);

                // Dual pane state
                const dualPane = ref(false);
                const pane2Path = ref(null);
                const pane2Entries = ref([]);
                const pane2Loading = ref(false);
                const pane2Error = ref(null);
                const pane2SortColumn = ref('name');
                const pane2SortAsc = ref(true);

                // View mode state
                const viewMode = ref('file'); // 'file' or 'media'

                // Audio player state
                const queue = ref([]);
                const currentIndex = ref(-1);
                const isPlaying = ref(false);
                const currentTime = ref(0);
                const duration = ref(0);
                const crossfadeEnabled = ref(true);
                const showQueue = ref(false);
                const isMuted = ref(false);

                // Cast state (backend-based)
                const showCastPanel = ref(false);
                const castDevices = ref([]);
                const castScanning = ref(false);
                const isCasting = ref(false);
                const castingTo = ref(null);
                let castStatusInterval = null;

                // Metadata refresh state
                const metadataStatus = ref({
                    scanning: false,
                    path: '',
                    current_file: '',
                    files_total: 0,
                    files_done: 0,
                    queue: [],
                    queue_length: 0
                });
                let metadataStatusInterval = null;
                const showMetadataQueuePanel = ref(false);

                // Computed property for metadata progress percent
                const metadataProgressPercent = computed(() => {
                    if (metadataStatus.value.files_total === 0) return 0;
                    return Math.round((metadataStatus.value.files_done / metadataStatus.value.files_total) * 100);
                });

                // Check if current path is being scanned
                const isCurrentPathScanning = computed(() => {
                    return metadataStatus.value.scanning && metadataStatus.value.path === currentPath.value;
                });

                // Check if current path is in the queue (returns position or 0)
                const currentPathQueuePosition = computed(() => {
                    if (!currentPath.value || !metadataStatus.value.queue) return 0;
                    const idx = metadataStatus.value.queue.indexOf(currentPath.value);
                    return idx >= 0 ? idx + 1 : 0;
                });

                // Check if current path is already queued or scanning
                const isCurrentPathQueued = computed(() => {
                    return isCurrentPathScanning.value || currentPathQueuePosition.value > 0;
                });

                // Button text for refresh button
                const refreshButtonText = computed(() => {
                    if (isCurrentPathScanning.value) {
                        return 'Scanning...';
                    }
                    if (currentPathQueuePosition.value > 0) {
                        return '#' + currentPathQueuePosition.value + ' in queue';
                    }
                    return 'Refresh Metadata';
                });

                // Check metadata status
                const checkMetadataStatus = async () => {
                    try {
                        const resp = await fetch('/api/metadata/status');
                        const data = await resp.json();
                        metadataStatus.value = data;
                        // Stop polling only if not scanning AND queue is empty
                        if (!data.scanning && data.queue_length === 0 && metadataStatusInterval) {
                            clearInterval(metadataStatusInterval);
                            metadataStatusInterval = null;
                        }
                    } catch (e) {
                        console.error('Failed to check metadata status:', e);
                    }
                };

                // Refresh metadata for current folder
                const refreshMetadata = async () => {
                    if (!currentPath.value) return;
                    try {
                        const resp = await fetch('/api/metadata/refresh', {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({ path: currentPath.value })
                        });
                        const data = await resp.json();
                        if (data.success) {
                            // Start polling for status
                            metadataStatus.value.scanning = true;
                            if (!metadataStatusInterval) {
                                metadataStatusInterval = setInterval(checkMetadataStatus, 500);
                            }
                        } else if (data.error) {
                            console.error('Metadata refresh error:', data.error);
                        }
                    } catch (e) {
                        console.error('Failed to refresh metadata:', e);
                    }
                };

                // Toggle metadata queue panel
                const toggleMetadataQueuePanel = () => {
                    showMetadataQueuePanel.value = !showMetadataQueuePanel.value;
                };

                // Remove a folder from the metadata queue
                const removeFromMetadataQueue = async (path) => {
                    try {
                        const resp = await fetch('/api/metadata/queue?path=' + encodeURIComponent(path), {
                            method: 'DELETE'
                        });
                        if (resp.ok) {
                            // Refresh status to update queue display
                            await checkMetadataStatus();
                        }
                    } catch (e) {
                        console.error('Failed to remove from queue:', e);
                    }
                };

                // Move a folder to the top of the queue
                const prioritizeInQueue = async (path) => {
                    try {
                        const resp = await fetch('/api/metadata/queue/prioritize', {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({ path: path })
                        });
                        if (resp.ok) {
                            // Refresh status to update queue display
                            await checkMetadataStatus();
                        }
                    } catch (e) {
                        console.error('Failed to prioritize in queue:', e);
                    }
                };

                // Cancel current metadata scan
                const cancelMetadataScan = async () => {
                    try {
                        const resp = await fetch('/api/metadata/cancel', {
                            method: 'POST'
                        });
                        if (resp.ok) {
                            // Refresh status to update display
                            await checkMetadataStatus();
                        }
                    } catch (e) {
                        console.error('Failed to cancel scan:', e);
                    }
                };

                // Check metadata status on mount (in case refresh is already running)
                checkMetadataStatus();

                // Scan for cast devices via backend (all devices - any Chromecast can play audio)
                const scanCastDevices = async () => {
                    castScanning.value = true;
                    try {
                        const resp = await fetch('/api/cast/devices');
                        const data = await resp.json();
                        if (data.devices) {
                            castDevices.value = data.devices;
                        }
                    } catch (e) {
                        console.error('Failed to scan cast devices:', e);
                    }
                    castScanning.value = false;
                };

                // Connect to a cast device
                const connectCastDevice = async (device) => {
                    try {
                        const resp = await fetch('/api/cast/connect', {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({ uuid: device.uuid })
                        });
                        const data = await resp.json();
                        if (data.success) {
                            isCasting.value = true;
                            castingTo.value = device.name;
                            showCastPanel.value = false;
                            // Pause local audio
                            const localAudio = getActiveAudioElement();
                            if (localAudio) {
                                localAudio.pause();
                            }
                            // Start status polling
                            startCastStatusPolling();
                            // Cast current track if one is loaded
                            if (currentTrack.value) {
                                castCurrentTrack();
                            }
                        } else {
                            console.error('Failed to connect:', data.error);
                        }
                    } catch (e) {
                        console.error('Failed to connect to cast device:', e);
                    }
                };

                // Start polling cast status
                const startCastStatusPolling = () => {
                    if (castStatusInterval) return;
                    castStatusInterval = setInterval(async () => {
                        if (!isCasting.value) {
                            clearInterval(castStatusInterval);
                            castStatusInterval = null;
                            return;
                        }
                        try {
                            const resp = await fetch('/api/cast/status');
                            const status = await resp.json();
                            if (status.connected) {
                                if (status.player_state === 'PLAYING') {
                                    isPlaying.value = true;
                                } else if (status.player_state === 'PAUSED') {
                                    isPlaying.value = false;
                                }
                                currentTime.value = status.current_time;
                                duration.value = status.duration;
                            } else {
                                // Connection lost
                                isCasting.value = false;
                                castingTo.value = null;
                            }
                        } catch (e) {
                            console.error('Failed to get cast status:', e);
                        }
                    }, 1000);
                };

                // Image viewer state
                const viewingImage = ref(null);

                // Video player state
                const videoFile = ref(null);
                const videoRef = ref(null);
                const videoPlaying = ref(false);
                const videoCurrentTime = ref(0);
                const videoDuration = ref(0);
                const videoMuted = ref(false);
                const isVideoCasting = ref(false);
                const videoCastingTo = ref(null);
                let videoCastSession = null;

                // Playlist state
                const showPlaylistMenu = ref(false);
                const playlistMenuX = ref(0);
                const playlistMenuY = ref(0);
                const playlistMenuSong = ref(null);
                const playlistMenuPane2 = ref(false);
                const availablePlaylists = ref([]);
                const viewingPlaylist = ref(null);
                const playlistSongs = ref([]);

                // Audio elements
                const audioA = ref(null);
                const audioB = ref(null);
                const activeAudio = ref('A');
                const crossfading = ref(false);

                // Web Audio for crossfade
                let audioContext = null;
                let gainNodeA = null;
                let gainNodeB = null;

                // Computed properties
                const currentTrack = computed(() =>
                    currentIndex.value >= 0 && currentIndex.value < queue.value.length
                        ? queue.value[currentIndex.value]
                        : null
                );

                const progressPercent = computed(() =>
                    duration.value > 0 ? (currentTime.value / duration.value) * 100 : 0
                );

                const pathParts = computed(() => {
                    if (!currentPath.value) return [];
                    return currentPath.value.split(/[\\/]/).filter(p => p);
                });

                const folderCount = computed(() => entries.value.filter(e => e.type === 'dir').length);
                const fileCount = computed(() => entries.value.filter(e => e.type === 'file').length);
                const totalSize = computed(() => entries.value.filter(e => e.type === 'file').reduce((sum, e) => sum + e.size, 0));

                const sortedEntries = computed(() => {
                    const sorted = [...entries.value].sort((a, b) => {
                        if (a.type !== b.type) return a.type === 'dir' ? -1 : 1;
                        let cmp = 0;
                        switch (sortColumn.value) {
                            case 'name':
                                cmp = a.name.localeCompare(b.name, undefined, { sensitivity: 'base' });
                                break;
                            case 'type':
                                cmp = getExtension(a.name).localeCompare(getExtension(b.name));
                                break;
                            case 'size':
                                cmp = a.size - b.size;
                                break;
                            case 'modified':
                                cmp = new Date(a.modified) - new Date(b.modified);
                                break;
                        }
                        return sortAsc.value ? cmp : -cmp;
                    });
                    return sorted;
                });

                // Media view computed properties
                const imageEntries = computed(() => sortedEntries.value.filter(e => e.type === 'file' && isImage(e.name)));
                const audioEntries = computed(() => sortedEntries.value.filter(e => e.type === 'file' && isAudio(e.name)));
                const videoEntries = computed(() => sortedEntries.value.filter(e => e.type === 'file' && isVideo(e.name)));
                const otherEntries = computed(() => sortedEntries.value.filter(e => e.type === 'dir' || (!isImage(e.name) && !isAudio(e.name) && !isVideo(e.name))));

                // Image list for navigation (filtered to only images)
                const imageList = computed(() =>
                    sortedEntries.value.filter(e => e.type === 'file' && isImage(e.name))
                );

                const currentImageIndex = computed(() => {
                    if (!viewingImage.value) return -1;
                    return imageList.value.findIndex(e => e.name === viewingImage.value.name);
                });

                const canPrevImage = computed(() => currentImageIndex.value > 0);
                const canNextImage = computed(() =>
                    currentImageIndex.value >= 0 && currentImageIndex.value < imageList.value.length - 1
                );

                // Pane 2 computed properties
                const pane2PathParts = computed(() => {
                    if (!pane2Path.value) return [];
                    return pane2Path.value.split(/[\\/]/).filter(p => p);
                });

                const pane2FolderCount = computed(() => pane2Entries.value.filter(e => e.type === 'dir').length);
                const pane2FileCount = computed(() => pane2Entries.value.filter(e => e.type === 'file').length);
                const pane2TotalSize = computed(() => pane2Entries.value.filter(e => e.type === 'file').reduce((sum, e) => sum + e.size, 0));

                const pane2SortedEntries = computed(() => {
                    const sorted = [...pane2Entries.value].sort((a, b) => {
                        if (a.type !== b.type) return a.type === 'dir' ? -1 : 1;
                        let cmp = 0;
                        switch (pane2SortColumn.value) {
                            case 'name':
                                cmp = a.name.localeCompare(b.name, undefined, { sensitivity: 'base' });
                                break;
                            case 'type':
                                cmp = getExtension(a.name).localeCompare(getExtension(b.name));
                                break;
                            case 'size':
                                cmp = a.size - b.size;
                                break;
                            case 'modified':
                                cmp = new Date(a.modified) - new Date(b.modified);
                                break;
                        }
                        return pane2SortAsc.value ? cmp : -cmp;
                    });
                    return sorted;
                });

                const pane2FullPath = (name) => pane2Path.value ? pane2Path.value + '\\' + name : name;

                // Helper functions
                const formatSize = (bytes) => {
                    if (bytes === 0) return '-';
                    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
                    const i = Math.floor(Math.log(bytes) / Math.log(1024));
                    return (bytes / Math.pow(1024, i)).toFixed(i > 0 ? 1 : 0) + ' ' + units[i];
                };

                const formatDate = (isoString) => {
                    const date = new Date(isoString);
                    return date.toLocaleDateString() + ' ' + date.toLocaleTimeString();
                };

                const formatTime = (seconds) => {
                    if (!seconds || isNaN(seconds)) return '0:00';
                    const m = Math.floor(seconds / 60);
                    const s = Math.floor(seconds % 60);
                    return m + ':' + s.toString().padStart(2, '0');
                };

                const getExtension = (name) => {
                    const idx = name.lastIndexOf('.');
                    return idx > 0 ? name.substring(idx + 1).toLowerCase() : '';
                };

                const isAudio = (name) => AUDIO_EXTENSIONS.includes(getExtension(name));
                const isImage = (name) => IMAGE_EXTENSIONS.includes(getExtension(name));
                const isVideo = (name) => VIDEO_EXTENSIONS.includes(getExtension(name));
                const isPlaylist = (name) => {
                    const ext = getExtension(name);
                    return ext === 'm3u8' || ext === 'm3u';
                };

                const playlistMenuStyle = computed(() => ({
                    top: playlistMenuY.value + 'px',
                    left: playlistMenuX.value + 'px'
                }));

                const fullPath = (name) => {
                    const sep = currentPath.value.includes('\\') ? '\\' : '/';
                    return currentPath.value + sep + name;
                };

                const sortIndicator = (col) => sortColumn.value === col ? (sortAsc.value ? '▲' : '▼') : '';

                // File browser functions
                const loadRoots = async (updateHash = true) => {
                    loading.value = true;
                    error.value = null;
                    currentPath.value = null;
                    try {
                        const resp = await fetch('/api/roots');
                        const data = await resp.json();
                        if (data.error) throw new Error(data.error);
                        roots.value = data.roots;
                        if (updateHash) {
                            history.pushState(null, '', window.location.pathname);
                        }
                    } catch (e) {
                        error.value = 'Failed to load: ' + e.message;
                    }
                    loading.value = false;
                };

                const browse = async (path, updateHash = true) => {
                    loading.value = true;
                    error.value = null;
                    try {
                        // Include metadata param when in media view mode
                        const metaParam = viewMode.value === 'media' ? '&metadata=true' : '';
                        const resp = await fetch('/api/browse?path=' + encodeURIComponent(path) + metaParam);
                        const data = await resp.json();
                        if (data.error) throw new Error(data.error);
                        currentPath.value = data.path;
                        entries.value = data.entries;
                        sortColumn.value = 'name';
                        sortAsc.value = true;
                        if (updateHash) {
                            history.pushState(null, '', '#' + encodeURIComponent(data.path));
                        }
                    } catch (e) {
                        error.value = 'Failed to load: ' + e.message;
                    }
                    loading.value = false;
                };

                // Browse with metadata (for media view)
                const browseWithMetadata = async (path) => {
                    loading.value = true;
                    error.value = null;
                    try {
                        const resp = await fetch('/api/browse?path=' + encodeURIComponent(path) + '&metadata=true');
                        const data = await resp.json();
                        if (data.error) throw new Error(data.error);
                        currentPath.value = data.path;
                        entries.value = data.entries;
                        sortColumn.value = 'name';
                        sortAsc.value = true;
                    } catch (e) {
                        error.value = 'Failed to load: ' + e.message;
                    }
                    loading.value = false;
                };

                const browseTo = (parts) => {
                    const sep = currentPath.value.includes('\\') ? '\\' : '/';
                    let path = parts.join(sep);
                    if (currentPath.value.match(/^[a-zA-Z]:/)) {
                        // For Windows paths, ensure drive root has trailing separator (P:\ not P:)
                        path = parts[0] + sep + (parts.length > 1 ? parts.slice(1).join(sep) : '');
                    }
                    browse(path);
                };

                const changeSort = (column) => {
                    if (sortColumn.value === column) {
                        sortAsc.value = !sortAsc.value;
                    } else {
                        sortColumn.value = column;
                        sortAsc.value = true;
                    }
                };

                // Dual pane toggle and functions
                const toggleDualPane = () => {
                    dualPane.value = !dualPane.value;
                };

                // View mode toggle
                const toggleViewMode = () => {
                    viewMode.value = viewMode.value === 'file' ? 'media' : 'file';
                    // Reload with metadata when switching to media view
                    if (viewMode.value === 'media' && currentPath.value) {
                        browseWithMetadata(currentPath.value);
                    }
                };

                // Format duration in seconds to mm:ss
                const formatDuration = (seconds) => {
                    if (!seconds) return '-';
                    const mins = Math.floor(seconds / 60);
                    const secs = seconds % 60;
                    return mins + ':' + String(secs).padStart(2, '0');
                };

                // Handle thumbnail load error (show placeholder)
                const handleThumbError = (e) => {
                    e.target.src = 'data:image/svg+xml,' + encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100" viewBox="0 0 100 100"><rect fill="%2321262d" width="100" height="100"/><text x="50" y="55" text-anchor="middle" fill="%236e7681" font-size="12">No Preview</text></svg>');
                };

                const loadRoots2 = () => {
                    pane2Path.value = null;
                    pane2Entries.value = [];
                    pane2Error.value = null;
                };

                const browse2 = async (path) => {
                    pane2Loading.value = true;
                    pane2Error.value = null;
                    try {
                        const resp = await fetch('/api/browse?path=' + encodeURIComponent(path));
                        const data = await resp.json();
                        if (data.error) throw new Error(data.error);
                        pane2Path.value = data.path;
                        pane2Entries.value = data.entries;
                        pane2SortColumn.value = 'name';
                        pane2SortAsc.value = true;
                    } catch (e) {
                        pane2Error.value = 'Failed to load: ' + e.message;
                    }
                    pane2Loading.value = false;
                };

                const browseTo2 = (parts) => {
                    const sep = pane2Path.value.includes('\\') ? '\\' : '/';
                    let path = parts.join(sep);
                    if (pane2Path.value.match(/^[a-zA-Z]:/)) {
                        // For Windows paths, ensure drive root has trailing separator (P:\ not P:)
                        path = parts[0] + sep + (parts.length > 1 ? parts.slice(1).join(sep) : '');
                    }
                    browse2(path);
                };

                const changeSort2 = (column) => {
                    if (pane2SortColumn.value === column) {
                        pane2SortAsc.value = !pane2SortAsc.value;
                    } else {
                        pane2SortColumn.value = column;
                        pane2SortAsc.value = true;
                    }
                };

                const sortIndicator2 = (column) => {
                    if (pane2SortColumn.value !== column) return '';
                    return pane2SortAsc.value ? '▲' : '▼';
                };

                // Pane 2 media functions (use same players, just different path)
                const openImage2 = (entry) => {
                    // Use pane2Path for the full path
                    viewingImage.value = { ...entry, _pane2Path: pane2Path.value };
                };

                const openVideo2 = (entry) => {
                    videoFile.value = { ...entry, _pane2Path: pane2Path.value };
                    videoPlaying.value = false;
                    videoCurrentTime.value = 0;
                    videoDuration.value = 0;
                };

                const playNow2 = (entry) => {
                    const track = { path: pane2FullPath(entry.name), name: entry.name };
                    queue.value = [track];
                    currentIndex.value = -1;
                    playTrack(0);
                };

                const addToQueueTop2 = (entry) => {
                    const track = { path: pane2FullPath(entry.name), name: entry.name };
                    const insertAt = currentIndex.value >= 0 ? currentIndex.value + 1 : 0;
                    queue.value.splice(insertAt, 0, track);
                    if (currentIndex.value < 0 && queue.value.length === 1) {
                        playTrack(0);
                    }
                    saveState();
                };

                const addToQueueBottom2 = (entry) => {
                    const track = { path: pane2FullPath(entry.name), name: entry.name };
                    queue.value.push(track);
                    if (currentIndex.value < 0 && queue.value.length === 1) {
                        playTrack(0);
                    }
                    saveState();
                };

                // Image viewer functions
                const openImage = (entry) => {
                    viewingImage.value = entry;
                };

                const closeImage = () => {
                    viewingImage.value = null;
                };

                const prevImage = () => {
                    const idx = currentImageIndex.value;
                    if (idx > 0) {
                        viewingImage.value = imageList.value[idx - 1];
                    }
                };

                const nextImage = () => {
                    const idx = currentImageIndex.value;
                    if (idx >= 0 && idx < imageList.value.length - 1) {
                        viewingImage.value = imageList.value[idx + 1];
                    }
                };

                // Keyboard handler for image viewer
                const handleKeydown = (e) => {
                    if (videoFile.value && e.key === 'Escape') {
                        closeVideo();
                        return;
                    }
                    if (!viewingImage.value) return;
                    if (e.key === 'Escape') closeImage();
                    if (e.key === 'ArrowLeft') prevImage();
                    if (e.key === 'ArrowRight') nextImage();
                };

                // Video player functions
                const videoProgressPercent = computed(() =>
                    videoDuration.value > 0 ? (videoCurrentTime.value / videoDuration.value) * 100 : 0
                );

                const openVideo = (entry) => {
                    videoFile.value = entry;
                    videoPlaying.value = false;
                    videoCurrentTime.value = 0;
                    videoDuration.value = 0;
                };

                const closeVideo = () => {
                    // Stop casting if active
                    if (isVideoCasting.value && videoCastSession) {
                        videoCastSession.endSession(true);
                        videoCastSession = null;
                        isVideoCasting.value = false;
                        videoCastingTo.value = null;
                    }
                    if (videoRef.value) {
                        videoRef.value.pause();
                    }
                    videoFile.value = null;
                    videoPlaying.value = false;
                };

                const toggleVideoPlay = () => {
                    if (!videoRef.value) return;
                    if (videoPlaying.value) {
                        videoRef.value.pause();
                    } else {
                        videoRef.value.play();
                    }
                };

                const onVideoTimeUpdate = () => {
                    if (videoRef.value) {
                        videoCurrentTime.value = videoRef.value.currentTime;
                    }
                };

                const onVideoMetadata = () => {
                    if (videoRef.value) {
                        videoDuration.value = videoRef.value.duration;
                    }
                };

                const onVideoPlay = () => { videoPlaying.value = true; };
                const onVideoPause = () => { videoPlaying.value = false; };
                const onVideoEnded = () => { videoPlaying.value = false; };

                const seekVideo = (event) => {
                    if (!videoRef.value) return;
                    const rect = event.currentTarget.getBoundingClientRect();
                    const percent = (event.clientX - rect.left) / rect.width;
                    videoRef.value.currentTime = percent * videoDuration.value;
                };

                const toggleVideoMute = () => {
                    videoMuted.value = !videoMuted.value;
                    if (videoRef.value) {
                        videoRef.value.muted = videoMuted.value;
                    }
                };

                // Video casting functions
                const getVideoContentType = (filename) => {
                    const ext = filename.toLowerCase().split('.').pop();
                    const types = {
                        'mp4': 'video/mp4',
                        'webm': 'video/webm',
                        'ogv': 'video/ogg',
                        'mov': 'video/quicktime',
                        'avi': 'video/x-msvideo',
                        'mkv': 'video/x-matroska',
                        'm4v': 'video/mp4'
                    };
                    return types[ext] || 'video/mp4';
                };

                const openVideoCastPicker = async () => {
                    if (!castAvailable.value || !castContext) return;

                    try {
                        await castContext.requestSession();
                        // Session started, now load the video
                        videoCastSession = castContext.getCurrentSession();
                        if (videoCastSession && videoFile.value) {
                            castCurrentVideo();
                        }
                    } catch (err) {
                        if (err.code !== 'cancel') {
                            console.log('Video cast request failed:', err);
                        }
                    }
                };

                const castCurrentVideo = () => {
                    if (!videoCastSession || !videoFile.value) return;

                    // Pause local video
                    if (videoRef.value) {
                        videoRef.value.pause();
                    }

                    const videoPath = fullPath(videoFile.value.name);
                    const streamUrl = window.location.origin + '/api/video?path=' + encodeURIComponent(videoPath);
                    const contentType = getVideoContentType(videoFile.value.name);

                    console.log('Casting video:', streamUrl, 'as', contentType);

                    const mediaInfo = new chrome.cast.media.MediaInfo(streamUrl, contentType);
                    mediaInfo.metadata = new chrome.cast.media.GenericMediaMetadata();
                    mediaInfo.metadata.title = videoFile.value.name;

                    const request = new chrome.cast.media.LoadRequest(mediaInfo);
                    request.autoplay = true;
                    request.currentTime = videoCurrentTime.value;

                    videoCastSession.loadMedia(request).then(
                        () => {
                            console.log('Video loaded on cast device');
                            isVideoCasting.value = true;
                            videoCastingTo.value = videoCastSession.getCastDevice().friendlyName;
                            videoPlaying.value = true;
                        },
                        (err) => {
                            console.error('Failed to load video on cast:', err);
                            isVideoCasting.value = false;
                        }
                    );
                };

                const stopVideoCasting = () => {
                    if (videoCastSession) {
                        videoCastSession.endSession(true);
                    }
                    videoCastSession = null;
                    isVideoCasting.value = false;
                    videoCastingTo.value = null;
                    // Resume local playback if video is still open
                    if (videoRef.value && videoFile.value) {
                        videoRef.value.play();
                    }
                };

                // Cast functions (backend-based)
                const toggleCastPanel = () => {
                    showCastPanel.value = !showCastPanel.value;
                    showQueue.value = false; // Close queue panel
                    // Auto-scan when opening
                    if (showCastPanel.value && castDevices.value.length === 0) {
                        scanCastDevices();
                    }
                };

                const stopCasting = async () => {
                    const wasPlaying = isPlaying.value;
                    const resumeTime = currentTime.value;

                    try {
                        await fetch('/api/cast/disconnect', { method: 'POST' });
                    } catch (e) {
                        console.error('Failed to disconnect:', e);
                    }
                    if (castStatusInterval) {
                        clearInterval(castStatusInterval);
                        castStatusInterval = null;
                    }
                    isCasting.value = false;
                    castingTo.value = null;
                    showCastPanel.value = false;

                    // Resume local playback if we were playing and have a track loaded
                    if (currentTrack.value) {
                        const audio = getActiveAudioElement();
                        // Ensure the track is loaded
                        if (!audio.src || !audio.src.includes(encodeURIComponent(currentTrack.value.path))) {
                            audio.src = '/api/stream?path=' + encodeURIComponent(currentTrack.value.path);
                        }
                        audio.currentTime = resumeTime;
                        if (wasPlaying) {
                            audio.play().then(() => {
                                isPlaying.value = true;
                            }).catch(e => {
                                console.error('Failed to resume playback:', e);
                            });
                        }
                    }
                };

                const castCurrentTrack = async () => {
                    if (!isCasting.value || !currentTrack.value) return;

                    try {
                        const resp = await fetch('/api/cast/play', {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({
                                path: currentTrack.value.path,
                                title: currentTrack.value.name
                            })
                        });
                        const data = await resp.json();
                        if (data.success) {
                            console.log('Media loaded on cast device:', data.media_url);
                            isPlaying.value = true;
                        } else {
                            console.error('Failed to cast:', data.error);
                        }
                        console.log('Cast response:', data);
                    } catch (e) {
                        console.error('Failed to cast media:', e);
                    }
                };

                const castTogglePlay = async () => {
                    if (!isCasting.value) return;
                    try {
                        if (isPlaying.value) {
                            const resp = await fetch('/api/cast/pause', { method: 'POST' });
                            if (resp.ok) isPlaying.value = false;
                        } else {
                            const resp = await fetch('/api/cast/resume', { method: 'POST' });
                            if (resp.ok) isPlaying.value = true;
                        }
                    } catch (e) {
                        console.error('Failed to toggle cast play:', e);
                    }
                };

                const castSeek = async (percent) => {
                    if (!isCasting.value) return;
                    // Convert percent to seconds using duration
                    const seekTime = percent * duration.value;
                    try {
                        await fetch('/api/cast/seek', {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({ position: seekTime })
                        });
                        currentTime.value = seekTime;
                    } catch (e) {
                        console.error('Failed to seek on cast:', e);
                    }
                };

                // Audio player functions
                const initAudioContext = () => {
                    if (audioContext) return;
                    try {
                        audioContext = new (window.AudioContext || window.webkitAudioContext)();

                        const sourceA = audioContext.createMediaElementSource(audioA.value);
                        gainNodeA = audioContext.createGain();
                        sourceA.connect(gainNodeA);
                        gainNodeA.connect(audioContext.destination);

                        const sourceB = audioContext.createMediaElementSource(audioB.value);
                        gainNodeB = audioContext.createGain();
                        sourceB.connect(gainNodeB);
                        gainNodeB.connect(audioContext.destination);
                    } catch (e) {
                        console.warn('Web Audio API not available, crossfade will use volume fallback');
                    }
                };

                const getActiveAudioElement = () => activeAudio.value === 'A' ? audioA.value : audioB.value;
                const getInactiveAudioElement = () => activeAudio.value === 'A' ? audioB.value : audioA.value;
                const getActiveGain = () => activeAudio.value === 'A' ? gainNodeA : gainNodeB;
                const getInactiveGain = () => activeAudio.value === 'A' ? gainNodeB : gainNodeA;

                const playTrack = (index) => {
                    if (index < 0 || index >= queue.value.length) return;

                    initAudioContext();
                    if (audioContext && audioContext.state === 'suspended') {
                        audioContext.resume();
                    }

                    currentIndex.value = index;
                    const track = queue.value[index];
                    const audio = getActiveAudioElement();

                    audio.src = '/api/stream?path=' + encodeURIComponent(track.path);
                    audio.play().then(() => {
                        isPlaying.value = true;
                    }).catch(e => console.error('Play error:', e));

                    saveState();
                };

                const playNow = (entry) => {
                    const track = { path: fullPath(entry.name), name: entry.name };
                    queue.value = [track];
                    currentIndex.value = -1;
                    playTrack(0);
                };

                const addToQueueTop = (entry) => {
                    const track = { path: fullPath(entry.name), name: entry.name };
                    const insertAt = currentIndex.value >= 0 ? currentIndex.value + 1 : 0;
                    queue.value.splice(insertAt, 0, track);
                    if (currentIndex.value < 0 && queue.value.length === 1) {
                        playTrack(0);
                    }
                    saveState();
                };

                const addToQueueBottom = (entry) => {
                    const track = { path: fullPath(entry.name), name: entry.name };
                    queue.value.push(track);
                    if (currentIndex.value < 0 && queue.value.length === 1) {
                        playTrack(0);
                    }
                    saveState();
                };

                const togglePlay = () => {
                    // If casting, control the Cast device
                    if (isCasting.value) {
                        castTogglePlay();
                        return;
                    }
                    const audio = getActiveAudioElement();
                    if (isPlaying.value) {
                        audio.pause();
                        isPlaying.value = false;
                    } else if (currentTrack.value) {
                        audio.play().then(() => {
                            isPlaying.value = true;
                        });
                    }
                };

                const playNext = () => {
                    if (currentIndex.value < queue.value.length - 1) {
                        if (crossfadeEnabled.value && isPlaying.value) {
                            startCrossfade(currentIndex.value + 1);
                        } else {
                            playTrack(currentIndex.value + 1);
                        }
                    }
                };

                const playPrevious = () => {
                    const audio = getActiveAudioElement();
                    if (audio.currentTime > 3) {
                        audio.currentTime = 0;
                    } else if (currentIndex.value > 0) {
                        playTrack(currentIndex.value - 1);
                    }
                };

                const startCrossfade = (nextIndex) => {
                    if (crossfading.value || nextIndex >= queue.value.length) return;

                    crossfading.value = true;
                    const track = queue.value[nextIndex];
                    const inactiveAudio = getInactiveAudioElement();
                    const activeGain = getActiveGain();
                    const inactiveGain = getInactiveGain();

                    // Load next track on inactive element
                    inactiveAudio.src = '/api/stream?path=' + encodeURIComponent(track.path);

                    if (audioContext && activeGain && inactiveGain) {
                        // Use Web Audio for smooth crossfade
                        inactiveGain.gain.value = 0;
                        inactiveAudio.play().then(() => {
                            const now = audioContext.currentTime;
                            activeGain.gain.linearRampToValueAtTime(0, now + CROSSFADE_DURATION);
                            inactiveGain.gain.linearRampToValueAtTime(1, now + CROSSFADE_DURATION);

                            setTimeout(() => {
                                getActiveAudioElement().pause();
                                activeAudio.value = activeAudio.value === 'A' ? 'B' : 'A';
                                currentIndex.value = nextIndex;
                                crossfading.value = false;
                                saveState();
                            }, CROSSFADE_DURATION * 1000);
                        });
                    } else {
                        // Fallback: simple volume crossfade
                        const activeAudioEl = getActiveAudioElement();
                        inactiveAudio.volume = 0;
                        inactiveAudio.play().then(() => {
                            const steps = 30;
                            const stepTime = (CROSSFADE_DURATION * 1000) / steps;
                            let step = 0;
                            const interval = setInterval(() => {
                                step++;
                                activeAudioEl.volume = Math.max(0, 1 - step / steps);
                                inactiveAudio.volume = Math.min(1, step / steps);
                                if (step >= steps) {
                                    clearInterval(interval);
                                    activeAudioEl.pause();
                                    activeAudioEl.volume = 1;
                                    activeAudio.value = activeAudio.value === 'A' ? 'B' : 'A';
                                    currentIndex.value = nextIndex;
                                    crossfading.value = false;
                                    saveState();
                                }
                            }, stepTime);
                        });
                    }
                };

                const seek = (event) => {
                    const bar = event.currentTarget;
                    const rect = bar.getBoundingClientRect();
                    const percent = (event.clientX - rect.left) / rect.width;
                    // If casting, seek on the Cast device
                    if (isCasting.value) {
                        castSeek(percent);
                        return;
                    }
                    const audio = getActiveAudioElement();
                    audio.currentTime = percent * audio.duration;
                };

                const onTimeUpdate = (event) => {
                    const audio = event.target;
                    if (audio === getActiveAudioElement()) {
                        currentTime.value = audio.currentTime;

                        // Check for crossfade trigger
                        if (crossfadeEnabled.value && !crossfading.value &&
                            audio.duration && audio.currentTime > 0 &&
                            audio.duration - audio.currentTime <= CROSSFADE_DURATION &&
                            currentIndex.value < queue.value.length - 1) {
                            startCrossfade(currentIndex.value + 1);
                        }
                    }
                };

                const onMetadata = (event) => {
                    const audio = event.target;
                    if (audio === getActiveAudioElement()) {
                        duration.value = audio.duration;
                    }
                };

                const onEnded = (event) => {
                    const audio = event.target;
                    if (audio === getActiveAudioElement() && !crossfading.value) {
                        if (currentIndex.value < queue.value.length - 1) {
                            playTrack(currentIndex.value + 1);
                        } else {
                            isPlaying.value = false;
                        }
                    }
                };

                // Mute control
                const toggleMute = () => {
                    isMuted.value = !isMuted.value;
                    if (audioA.value) audioA.value.muted = isMuted.value;
                    if (audioB.value) audioB.value.muted = isMuted.value;
                    saveState();
                };

                // Queue management
                const toggleQueue = () => {
                    showQueue.value = !showQueue.value;
                };

                const removeFromQueue = (index) => {
                    if (index === currentIndex.value) {
                        // Removing currently playing track
                        if (queue.value.length > 1) {
                            if (index < queue.value.length - 1) {
                                queue.value.splice(index, 1);
                                playTrack(index);
                            } else {
                                queue.value.splice(index, 1);
                                currentIndex.value = -1;
                                isPlaying.value = false;
                                getActiveAudioElement().pause();
                            }
                        } else {
                            queue.value = [];
                            currentIndex.value = -1;
                            isPlaying.value = false;
                            getActiveAudioElement().pause();
                        }
                    } else {
                        if (index < currentIndex.value) {
                            currentIndex.value--;
                        }
                        queue.value.splice(index, 1);
                    }
                    saveState();
                };

                const clearQueue = () => {
                    queue.value = [];
                    currentIndex.value = -1;
                    isPlaying.value = false;
                    getActiveAudioElement()?.pause();
                    saveState();
                };

                const moveUp = (index) => {
                    if (index <= 0) return;
                    const item = queue.value.splice(index, 1)[0];
                    queue.value.splice(index - 1, 0, item);
                    if (currentIndex.value === index) currentIndex.value--;
                    else if (currentIndex.value === index - 1) currentIndex.value++;
                    saveState();
                };

                const moveDown = (index) => {
                    if (index >= queue.value.length - 1) return;
                    const item = queue.value.splice(index, 1)[0];
                    queue.value.splice(index + 1, 0, item);
                    if (currentIndex.value === index) currentIndex.value++;
                    else if (currentIndex.value === index + 1) currentIndex.value--;
                    saveState();
                };

                // Persistence
                const saveState = () => {
                    localStorage.setItem('q2-queue', JSON.stringify(queue.value));
                    localStorage.setItem('q2-currentIndex', currentIndex.value.toString());
                    localStorage.setItem('q2-crossfade', crossfadeEnabled.value.toString());
                    localStorage.setItem('q2-muted', isMuted.value.toString());
                };

                const loadState = () => {
                    try {
                        const savedQueue = localStorage.getItem('q2-queue');
                        if (savedQueue) queue.value = JSON.parse(savedQueue);

                        const savedIndex = localStorage.getItem('q2-currentIndex');
                        if (savedIndex) currentIndex.value = parseInt(savedIndex, 10);

                        const savedCrossfade = localStorage.getItem('q2-crossfade');
                        if (savedCrossfade) crossfadeEnabled.value = savedCrossfade === 'true';

                        const savedMuted = localStorage.getItem('q2-muted');
                        if (savedMuted) isMuted.value = savedMuted === 'true';
                    } catch (e) {
                        console.error('Failed to load state:', e);
                    }
                };

                // Playlist functions
                const openPlaylistMenu = async (event, entry, isPane2 = false) => {
                    const songPath = isPane2 ? pane2FullPath(entry.name) : fullPath(entry.name);
                    playlistMenuSong.value = { path: songPath, name: entry.name };
                    playlistMenuPane2.value = isPane2;

                    // Position popup near button
                    const rect = event.target.getBoundingClientRect();
                    playlistMenuX.value = Math.min(rect.left, window.innerWidth - 250);
                    playlistMenuY.value = rect.bottom + 5;

                    // Fetch playlists with contains info
                    try {
                        const resp = await fetch('/api/playlist/check?song=' + encodeURIComponent(songPath));
                        const data = await resp.json();
                        availablePlaylists.value = data.playlists || [];
                    } catch (e) {
                        console.error('Failed to load playlists:', e);
                        availablePlaylists.value = [];
                    }

                    showPlaylistMenu.value = true;
                };

                const closePlaylistMenu = () => {
                    showPlaylistMenu.value = false;
                    playlistMenuSong.value = null;
                };

                const addToPlaylist = async (playlistPath) => {
                    if (!playlistMenuSong.value) return;

                    try {
                        await fetch('/api/playlist/add', {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({
                                playlist: playlistPath,
                                song: playlistMenuSong.value.path,
                                title: playlistMenuSong.value.name,
                                duration: 0
                            })
                        });
                    } catch (e) {
                        console.error('Failed to add to playlist:', e);
                    }

                    closePlaylistMenu();
                };

                const createNewPlaylist = async () => {
                    const name = prompt('Enter playlist name:');
                    if (!name) return;

                    try {
                        const createResp = await fetch('/api/playlist', {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({ name })
                        });
                        const createData = await createResp.json();

                        if (createData.success && playlistMenuSong.value) {
                            await addToPlaylist(createData.path);
                        } else {
                            closePlaylistMenu();
                        }
                    } catch (e) {
                        console.error('Failed to create playlist:', e);
                        closePlaylistMenu();
                    }
                };

                const openPlaylist = async (entry) => {
                    const path = fullPath(entry.name);
                    try {
                        const resp = await fetch('/api/playlist?path=' + encodeURIComponent(path));
                        const data = await resp.json();
                        if (data.error) {
                            console.error('Failed to load playlist:', data.error);
                            return;
                        }
                        viewingPlaylist.value = { name: data.name, path: path };
                        playlistSongs.value = data.songs || [];
                    } catch (e) {
                        console.error('Failed to load playlist:', e);
                    }
                };

                const closePlaylistViewer = () => {
                    viewingPlaylist.value = null;
                    playlistSongs.value = [];
                };

                const playAllFromPlaylist = () => {
                    if (playlistSongs.value.length === 0) return;
                    queue.value = playlistSongs.value.map(s => ({ path: s.path, name: s.title }));
                    currentIndex.value = 0;
                    playTrack(0);
                    closePlaylistViewer();
                };

                const shuffleAndPlayPlaylist = () => {
                    if (playlistSongs.value.length === 0) return;
                    const shuffled = [...playlistSongs.value].sort(() => Math.random() - 0.5);
                    queue.value = shuffled.map(s => ({ path: s.path, name: s.title }));
                    currentIndex.value = 0;
                    playTrack(0);
                    closePlaylistViewer();
                };

                const playSongFromPlaylist = (index) => {
                    const song = playlistSongs.value[index];
                    if (!song) return;
                    queue.value = [{ path: song.path, name: song.title }];
                    currentIndex.value = 0;
                    playTrack(0);
                };

                const movePlaylistSongUp = async (index) => {
                    if (index <= 0 || !viewingPlaylist.value) return;
                    try {
                        await fetch('/api/playlist/reorder', {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({
                                playlist: viewingPlaylist.value.path,
                                from_index: index,
                                to_index: index - 1
                            })
                        });
                        // Refresh playlist
                        const resp = await fetch('/api/playlist?path=' + encodeURIComponent(viewingPlaylist.value.path));
                        const data = await resp.json();
                        playlistSongs.value = data.songs || [];
                    } catch (e) {
                        console.error('Failed to reorder:', e);
                    }
                };

                const movePlaylistSongDown = async (index) => {
                    if (index >= playlistSongs.value.length - 1 || !viewingPlaylist.value) return;
                    try {
                        await fetch('/api/playlist/reorder', {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({
                                playlist: viewingPlaylist.value.path,
                                from_index: index,
                                to_index: index + 1
                            })
                        });
                        // Refresh playlist
                        const resp = await fetch('/api/playlist?path=' + encodeURIComponent(viewingPlaylist.value.path));
                        const data = await resp.json();
                        playlistSongs.value = data.songs || [];
                    } catch (e) {
                        console.error('Failed to reorder:', e);
                    }
                };

                const removeFromPlaylist = async (index) => {
                    if (!viewingPlaylist.value) return;
                    try {
                        await fetch('/api/playlist/remove', {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({
                                playlist: viewingPlaylist.value.path,
                                index: index
                            })
                        });
                        // Refresh playlist
                        const resp = await fetch('/api/playlist?path=' + encodeURIComponent(viewingPlaylist.value.path));
                        const data = await resp.json();
                        playlistSongs.value = data.songs || [];
                    } catch (e) {
                        console.error('Failed to remove song:', e);
                    }
                };

                const deletePlaylist = async () => {
                    if (!viewingPlaylist.value) return;
                    if (!confirm('Delete playlist "' + viewingPlaylist.value.name + '"?')) return;
                    try {
                        await fetch('/api/playlist?path=' + encodeURIComponent(viewingPlaylist.value.path), {
                            method: 'DELETE'
                        });
                        closePlaylistViewer();
                        // Refresh current directory to update file list
                        if (currentPath.value) {
                            browse(currentPath.value);
                        }
                    } catch (e) {
                        console.error('Failed to delete playlist:', e);
                    }
                };

                // Watch crossfade setting
                watch(crossfadeEnabled, () => saveState());

                // Handle URL hash navigation
                const navigateFromHash = () => {
                    const hash = window.location.hash;
                    if (hash && hash.length > 1) {
                        const path = decodeURIComponent(hash.substring(1));
                        browse(path, false);
                    } else {
                        loadRoots(false);
                    }
                };

                // Lifecycle
                onMounted(() => {
                    loadState();
                    // Apply mute state to audio elements
                    if (audioA.value) audioA.value.muted = isMuted.value;
                    if (audioB.value) audioB.value.muted = isMuted.value;
                    // Navigate based on URL hash (or load roots if no hash)
                    navigateFromHash();
                    // Add keyboard listener for image viewer
                    document.addEventListener('keydown', handleKeydown);
                    // Handle browser back/forward buttons
                    window.addEventListener('popstate', navigateFromHash);
                });

                return {
                    // File browser
                    roots, currentPath, entries, loading, error,
                    sortColumn, sortAsc, pathParts, folderCount, fileCount, totalSize, sortedEntries,
                    formatSize, formatDate, getExtension, isAudio, isImage, isVideo, isPlaylist, fullPath, sortIndicator,
                    loadRoots, browse, browseTo, changeSort,

                    // Media view
                    viewMode, toggleViewMode,
                    imageEntries, audioEntries, videoEntries, otherEntries,
                    formatDuration, handleThumbError,

                    // Dual pane
                    dualPane, toggleDualPane,
                    pane2Path, pane2Entries, pane2Loading, pane2Error,
                    pane2SortColumn, pane2SortAsc, pane2PathParts, pane2FolderCount, pane2FileCount, pane2TotalSize,
                    pane2SortedEntries, pane2FullPath, sortIndicator2,
                    loadRoots2, browse2, browseTo2, changeSort2,
                    openImage2, openVideo2, playNow2, addToQueueTop2, addToQueueBottom2,

                    // Image viewer
                    viewingImage, canPrevImage, canNextImage,
                    openImage, closeImage, prevImage, nextImage,

                    // Video player
                    videoFile, videoRef, videoPlaying, videoCurrentTime, videoDuration, videoMuted,
                    videoProgressPercent, formatTime,
                    openVideo, closeVideo, toggleVideoPlay, seekVideo, toggleVideoMute,
                    onVideoTimeUpdate, onVideoMetadata, onVideoPlay, onVideoPause, onVideoEnded,
                    isVideoCasting, videoCastingTo, openVideoCastPicker, stopVideoCasting,

                    // Audio player
                    queue, currentIndex, currentTrack, isPlaying, currentTime, duration,
                    progressPercent, crossfadeEnabled, showQueue, isMuted,
                    audioA, audioB,
                    playNow, addToQueueTop, addToQueueBottom,
                    togglePlay, playNext, playPrevious, seek, toggleMute,
                    onTimeUpdate, onMetadata, onEnded,
                    toggleQueue, removeFromQueue, clearQueue, moveUp, moveDown,

                    // Chromecast (backend-based)
                    showCastPanel, castDevices, castScanning, isCasting, castingTo,
                    toggleCastPanel, scanCastDevices, connectCastDevice, stopCasting,

                    // Playlists
                    showPlaylistMenu, playlistMenuStyle, availablePlaylists,
                    openPlaylistMenu, closePlaylistMenu, addToPlaylist, createNewPlaylist,
                    viewingPlaylist, playlistSongs, openPlaylist, closePlaylistViewer,
                    playAllFromPlaylist, shuffleAndPlayPlaylist, playSongFromPlaylist,
                    movePlaylistSongUp, movePlaylistSongDown, removeFromPlaylist, deletePlaylist,

                    // Metadata refresh
                    metadataStatus, metadataProgressPercent, refreshMetadata,
                    isCurrentPathScanning, isCurrentPathQueued, refreshButtonText,
                    showMetadataQueuePanel, toggleMetadataQueuePanel,
                    removeFromMetadataQueue, prioritizeInQueue, cancelMetadataScan
                };
            }
        }).mount('#app');
    </script>
</body>
</html>`

// browsePageHandler serves the file browser HTML page.
func browsePageHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(browsePageHTML))
}

// ColumnInfo holds information about a table column.
type ColumnInfo struct {
	CID     int
	Name    string
	Type    string
	NotNull bool
	Default *string
	PK      int
}

// TableInfo holds schema information for a table.
type TableInfo struct {
	Name    string
	SQL     string
	Columns []ColumnInfo
}

// IndexInfo holds information about an index.
type IndexInfo struct {
	Name   string
	Table  string
	SQL    string
	Unique bool
}

// makeSchemaHandler creates a handler for /schema.
func makeSchemaHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Get all tables
		rows, err := database.Query(`
			SELECT name, sql FROM sqlite_master
			WHERE type='table' AND name NOT LIKE 'sqlite_%'
			ORDER BY name
		`)
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var tables []TableInfo
		for rows.Next() {
			var t TableInfo
			var sql *string
			if err := rows.Scan(&t.Name, &sql); err != nil {
				continue
			}
			if sql != nil {
				t.SQL = *sql
			}
			tables = append(tables, t)
		}

		// Get column info for each table
		for i := range tables {
			colRows, err := database.Query(fmt.Sprintf("PRAGMA table_info(%s)", tables[i].Name))
			if err != nil {
				continue
			}
			for colRows.Next() {
				var col ColumnInfo
				var notNull int
				if err := colRows.Scan(&col.CID, &col.Name, &col.Type, &notNull, &col.Default, &col.PK); err != nil {
					continue
				}
				col.NotNull = notNull == 1
				tables[i].Columns = append(tables[i].Columns, col)
			}
			colRows.Close()
		}

		// Get all indexes
		idxRows, err := database.Query(`
			SELECT name, tbl_name, sql FROM sqlite_master
			WHERE type='index' AND name NOT LIKE 'sqlite_%'
			ORDER BY tbl_name, name
		`)
		if err != nil {
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		defer idxRows.Close()

		var indexes []IndexInfo
		for idxRows.Next() {
			var idx IndexInfo
			var sql *string
			if err := idxRows.Scan(&idx.Name, &idx.Table, &sql); err != nil {
				continue
			}
			if sql != nil {
				idx.SQL = *sql
				idx.Unique = strings.Contains(strings.ToUpper(*sql), "UNIQUE")
			}
			indexes = append(indexes, idx)
		}

		// Render HTML
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Q2 Database Schema</title>
    <style>
        * { box-sizing: border-box; }
        body { font-family: "Cascadia Code", "Fira Code", "JetBrains Mono", "SF Mono", Consolas, monospace; margin: 0; padding: 20px; background: #0d1117; color: #c9d1d9; }
        .container { max-width: 1000px; margin: 0 auto; }
        h1 { color: #58a6ff; margin-bottom: 30px; }
        h2 { color: #c9d1d9; margin-top: 30px; margin-bottom: 15px; border-bottom: 1px solid #30363d; padding-bottom: 5px; }
        .table-card { background: #161b22; border: 1px solid #30363d; border-radius: 6px; margin-bottom: 20px; overflow: hidden; }
        .table-header { background: #238636; color: white; padding: 15px 20px; font-size: 16px; font-weight: 600; }
        .table-header .icon { margin-right: 10px; }
        table { width: 100%; border-collapse: collapse; }
        th, td { padding: 12px 20px; text-align: left; border-bottom: 1px solid #21262d; }
        th { background: #0d1117; font-weight: 600; color: #8b949e; }
        tr:last-child td { border-bottom: none; }
        tr:hover { background: #1f2428; }
        .col-name { font-weight: 500; color: #c9d1d9; }
        .col-type { color: #7ee787; background: #23883622; padding: 2px 8px; border-radius: 4px; }
        .col-pk { background: #d29922; color: #0d1117; padding: 2px 8px; border-radius: 4px; font-size: 11px; font-weight: 600; margin-left: 8px; }
        .col-notnull { background: #f85149; color: white; padding: 2px 8px; border-radius: 4px; font-size: 11px; margin-left: 8px; }
        .col-default { color: #8b949e; font-size: 12px; }
        .sql-block { background: #0d1117; color: #7ee787; padding: 15px 20px; font-size: 12px; white-space: pre-wrap; word-break: break-all; border-top: 1px solid #30363d; }
        .index-card { background: #161b22; border: 1px solid #30363d; border-radius: 6px; margin-bottom: 10px; padding: 15px 20px; }
        .index-name { font-weight: 600; color: #c9d1d9; }
        .index-table { color: #8b949e; margin-left: 10px; }
        .index-unique { background: #238636; color: white; padding: 2px 8px; border-radius: 4px; font-size: 11px; margin-left: 8px; }
        .index-sql { font-size: 12px; color: #7ee787; margin-top: 8px; background: #0d1117; padding: 10px; border-radius: 4px; }
        .empty-message { color: #8b949e; font-style: italic; padding: 20px; }
    </style>
</head>
<body>
    <div class="container">
        <h1>&gt; schema_</h1>
        <h2>Tables</h2>
`)

		if len(tables) == 0 {
			fmt.Fprint(w, `<p class="empty-message">No tables found.</p>`)
		}

		for _, t := range tables {
			fmt.Fprintf(w, `<div class="table-card">
            <div class="table-header"><span class="icon">📊</span>%s</div>
            <table>
                <thead>
                    <tr>
                        <th>Column</th>
                        <th>Type</th>
                        <th>Constraints</th>
                        <th>Default</th>
                    </tr>
                </thead>
                <tbody>
`, t.Name)

			for _, col := range t.Columns {
				constraints := ""
				if col.PK > 0 {
					constraints += `<span class="col-pk">PK</span>`
				}
				if col.NotNull {
					constraints += `<span class="col-notnull">NOT NULL</span>`
				}
				defaultVal := "-"
				if col.Default != nil {
					defaultVal = fmt.Sprintf(`<span class="col-default">%s</span>`, *col.Default)
				}
				fmt.Fprintf(w, `                    <tr>
                        <td class="col-name">%s</td>
                        <td><span class="col-type">%s</span></td>
                        <td>%s</td>
                        <td>%s</td>
                    </tr>
`, col.Name, col.Type, constraints, defaultVal)
			}

			fmt.Fprintf(w, `                </tbody>
            </table>
            <div class="sql-block">%s</div>
        </div>
`, t.SQL)
		}

		fmt.Fprint(w, `<h2>Indexes</h2>`)

		if len(indexes) == 0 {
			fmt.Fprint(w, `<p class="empty-message">No indexes found.</p>`)
		}

		for _, idx := range indexes {
			unique := ""
			if idx.Unique {
				unique = `<span class="index-unique">UNIQUE</span>`
			}
			fmt.Fprintf(w, `<div class="index-card">
            <div><span class="index-name">%s</span><span class="index-table">on %s</span>%s</div>
            <div class="index-sql">%s</div>
        </div>
`, idx.Name, idx.Table, unique, idx.SQL)
		}

		fmt.Fprint(w, `    </div>
</body>
</html>`)
	}
}

// initDB initializes the database and runs migrations.
func initDB(baseDir string) (*db.DB, error) {
	// Ensure .q2 directory exists
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory %s: %w", baseDir, err)
	}

	dbPath := filepath.Join(baseDir, dbFile)
	database, err := db.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := database.Migrate(); err != nil {
		database.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return database, nil
}

// cleanPath trims whitespace and removes stray quote characters from shell escaping issues.
// Returns the cleaned path and true if non-empty, or empty string and false if empty.
func cleanPath(path string) (string, bool) {
	path = strings.TrimSpace(path)
	path = strings.Trim(path, `"'`)
	if path == "" {
		return "", false
	}
	return filepath.Clean(path), true
}

// normalizePath cleans the path and applies platform-specific normalization.
// On Windows, paths are lowercased for case-insensitive comparison.
// On Linux/macOS, paths are kept as-is for case-sensitive comparison.
func normalizePath(path string) string {
	path, _ = cleanPath(path)
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}

// addFolder adds the given folder path to the database.
// It ensures the folder exists and no duplicate entries are added.
// Case sensitivity matches the platform (case-insensitive on Windows, case-sensitive on Linux).
// Returns an error if the folder is empty, doesn't exist, or a database error occurs.
func addFolder(folder string, database *db.DB) error {
	folder, ok := cleanPath(folder)
	if !ok {
		return errors.New("folder cannot be empty")
	}

	// Check if folder exists
	info, err := os.Stat(folder)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("folder does not exist: %s", folder)
		}
		return fmt.Errorf("cannot access folder: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("path is not a directory: %s", folder)
	}

	// Normalize path for storage (lowercase on Windows)
	normalizedPath := normalizePath(folder)

	// Try to insert - will fail if duplicate due to UNIQUE constraint
	result := database.Write(
		"INSERT OR IGNORE INTO folders (path) VALUES (?)",
		normalizedPath,
	)
	if result.Err != nil {
		return result.Err
	}

	if result.RowsAffected == 0 {
		fmt.Printf("Folder %s already exists\n", folder)
	} else {
		fmt.Printf("Folder %s added\n", folder)
	}

	return nil
}

// removeFolder removes a folder from the database.
// Returns an error if the folder is empty or not found.
func removeFolder(folder string, database *db.DB) error {
	folder, ok := cleanPath(folder)
	if !ok {
		return errors.New("folder cannot be empty")
	}

	normalizedPath := normalizePath(folder)

	result := database.Write("DELETE FROM folders WHERE path = ?", normalizedPath)
	if result.Err != nil {
		return result.Err
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("folder not found: %s", folder)
	}

	fmt.Printf("Folder %s removed\n", folder)
	return nil
}

// listFolders retrieves and displays all stored folders from the database.
func listFolders(database *db.DB) error {
	rows, err := database.Query("SELECT path FROM folders ORDER BY path")
	if err != nil {
		return fmt.Errorf("failed to query folders: %w", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return fmt.Errorf("failed to read folder: %w", err)
		}
		fmt.Println(path)
		count++
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error reading folders: %w", err)
	}

	if count == 0 {
		fmt.Println("No folders stored")
	}

	return nil
}

// main parses subcommands and dispatches to the appropriate handler.
// Supported commands: addfolder, removefolder, listfolders, serve
func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  %s <command> [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Commands:\n")
		fmt.Fprintf(os.Stderr, "  addfolder	Add a folder to Q2\n")
		fmt.Fprintf(os.Stderr, "  removefolder	Remove a folder from Q2\n")
		fmt.Fprintf(os.Stderr, "  listfolders	List stored folders\n")
		fmt.Fprintf(os.Stderr, "  scan		Scan a folder for files\n")
		fmt.Fprintf(os.Stderr, "  serve		Start serving Q2\n")
	}

	if len(os.Args) < 2 {
		flag.Usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "addfolder":
		addFolderCmd := flag.NewFlagSet("addfolder", flag.ContinueOnError)

		addFolderCmd.Usage = func() {
			fmt.Fprintf(os.Stderr, "Usage: \n")
			fmt.Fprintf(os.Stderr, "  %s addfolder <folder>\n\n", os.Args[0])
			addFolderCmd.PrintDefaults()
		}
		if err := addFolderCmd.Parse(os.Args[2:]); err != nil {
			addFolderCmd.Usage()
			os.Exit(2)
		}

		args := addFolderCmd.Args()

		if len(args) != 1 {
			fmt.Fprintln(os.Stderr, "addfolder requires exactly one <folder>")
			addFolderCmd.Usage()
			os.Exit(2)
		}

		folder := args[0]

		database, err := initDB(q2Dir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error initializing database:", err)
			os.Exit(1)
		}
		defer database.Close()

		if err := addFolder(folder, database); err != nil {
			fmt.Fprintln(os.Stderr, "Error adding folder:", err)
			os.Exit(1)
		}

	case "removefolder":
		removeFolderCmd := flag.NewFlagSet("removefolder", flag.ContinueOnError)

		removeFolderCmd.Usage = func() {
			fmt.Fprintf(os.Stderr, "Usage: \n")
			fmt.Fprintf(os.Stderr, "  %s removefolder <folder>\n\n", os.Args[0])
			removeFolderCmd.PrintDefaults()
		}
		if err := removeFolderCmd.Parse(os.Args[2:]); err != nil {
			removeFolderCmd.Usage()
			os.Exit(2)
		}

		args := removeFolderCmd.Args()

		if len(args) != 1 {
			fmt.Fprintln(os.Stderr, "removefolder requires exactly one <folder>")
			removeFolderCmd.Usage()
			os.Exit(2)
		}

		folder := args[0]

		database, err := initDB(q2Dir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error initializing database:", err)
			os.Exit(1)
		}
		defer database.Close()

		if err := removeFolder(folder, database); err != nil {
			fmt.Fprintln(os.Stderr, "Error removing folder:", err)
			os.Exit(1)
		}

	case "listfolders":
		database, err := initDB(q2Dir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error initializing database:", err)
			os.Exit(1)
		}
		defer database.Close()

		if err := listFolders(database); err != nil {
			fmt.Fprintln(os.Stderr, "Error listing folders:", err)
			os.Exit(1)
		}

	case "scan":
		scanCmd := flag.NewFlagSet("scan", flag.ContinueOnError)

		scanCmd.Usage = func() {
			fmt.Fprintf(os.Stderr, "Usage: \n")
			fmt.Fprintf(os.Stderr, "  %s scan <folder>\n\n", os.Args[0])
			fmt.Fprintf(os.Stderr, "Scans a folder for files and adds them to the database.\n")
			fmt.Fprintf(os.Stderr, "The folder must be within a monitored folder.\n\n")
			scanCmd.PrintDefaults()
		}

		if err := scanCmd.Parse(os.Args[2:]); err != nil {
			scanCmd.Usage()
			os.Exit(2)
		}

		args := scanCmd.Args()

		if len(args) != 1 {
			fmt.Fprintln(os.Stderr, "scan requires exactly one <folder>")
			scanCmd.Usage()
			os.Exit(2)
		}

		folder := args[0]

		// Clean and validate the folder path
		folder, ok := cleanPath(folder)
		if !ok {
			fmt.Fprintln(os.Stderr, "Error: folder cannot be empty")
			os.Exit(1)
		}

		// Check if folder exists
		info, err := os.Stat(folder)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "Error: folder does not exist: %s\n", folder)
			} else {
				fmt.Fprintf(os.Stderr, "Error: cannot access folder: %v\n", err)
			}
			os.Exit(1)
		}
		if !info.IsDir() {
			fmt.Fprintf(os.Stderr, "Error: path is not a directory: %s\n", folder)
			os.Exit(1)
		}

		database, err := initDB(q2Dir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error initializing database:", err)
			os.Exit(1)
		}
		defer database.Close()

		// Find the parent monitored folder
		parentPath, folderID, err := scanner.FindParentFolder(database, folder)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Scanning %s (monitored folder: %s)...\n", folder, parentPath)

		// Perform the scan
		result, err := scanner.ScanFolder(database, folder, folderID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error scanning folder: %v\n", err)
			os.Exit(1)
		}

		// Report results
		fmt.Printf("Scan complete: %d added, %d updated, %d removed\n",
			result.FilesAdded, result.FilesUpdated, result.FilesRemoved)

		if len(result.Errors) > 0 {
			fmt.Printf("%d errors encountered:\n", len(result.Errors))
			for _, e := range result.Errors {
				fmt.Printf("  - %v\n", e)
			}
		}

	case "serve":
		serveCmd := flag.NewFlagSet("serve", flag.ContinueOnError)
		port := serveCmd.Int("port", 8090, "Port to listen on")

		serveCmd.Usage = func() {
			fmt.Fprintf(os.Stderr, "Usage: \n")
			fmt.Fprintf(os.Stderr, "  %s serve [options]\n\n", os.Args[0])
			fmt.Fprintf(os.Stderr, "Options:\n")
			serveCmd.PrintDefaults()
		}

		if err := serveCmd.Parse(os.Args[2:]); err != nil {
			serveCmd.Usage()
			os.Exit(2)
		}

		database, err := initDB(q2Dir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error initializing database:", err)
			os.Exit(1)
		}
		defer database.Close()

		fmt.Println("Q2")

		// Ensure playlists folder exists and is monitored
		playlistDir, err := ensurePlaylistsFolder(q2Dir, database)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Warning: could not initialize playlists folder:", err)
			playlistDir = filepath.Join(q2Dir, "playlists") // Use default path anyway
		}

		// Create cast manager - base URL will be set when first request comes in
		castMgr := cast.NewManager("")

		// Create ffmpeg manager for video transcoding
		ffmpegBinDir := filepath.Join(q2Dir, "bin")
		ffmpegMgr := ffmpeg.NewManager(ffmpegBinDir)

		// Set up HTTP handlers
		mux := http.NewServeMux()
		mux.HandleFunc("/", homeEndpoint)
		mux.HandleFunc("/browse", browsePageHandler)
		mux.HandleFunc("/schema", makeSchemaHandler(database))
		mux.HandleFunc("/api/roots", makeRootsHandler(database))
		mux.HandleFunc("/api/browse", makeBrowseHandler(database, q2Dir))
		mux.HandleFunc("/api/stream", makeStreamHandler(database))
		mux.HandleFunc("/api/image", makeImageHandler(database))
		mux.HandleFunc("/api/thumbnail", makeThumbnailHandler(database, q2Dir))
		mux.HandleFunc("/api/video", makeVideoHandler(database, ffmpegMgr))

		// Cast API endpoints
		mux.HandleFunc("/api/cast/devices", makeCastDevicesHandler(castMgr))
		mux.HandleFunc("/api/cast/connect", makeCastConnectHandler(castMgr))
		mux.HandleFunc("/api/cast/disconnect", makeCastDisconnectHandler(castMgr))
		mux.HandleFunc("/api/cast/play", makeCastPlayHandler(castMgr))
		mux.HandleFunc("/api/cast/pause", makeCastPauseHandler(castMgr))
		mux.HandleFunc("/api/cast/resume", makeCastResumeHandler(castMgr))
		mux.HandleFunc("/api/cast/stop", makeCastStopHandler(castMgr))
		mux.HandleFunc("/api/cast/seek", makeCastSeekHandler(castMgr))
		mux.HandleFunc("/api/cast/volume", makeCastVolumeHandler(castMgr))
		mux.HandleFunc("/api/cast/status", makeCastStatusHandler(castMgr))

		// Playlist API endpoints
		mux.HandleFunc("/api/playlists", makePlaylistsHandler(playlistDir))
		mux.HandleFunc("/api/playlist", makePlaylistHandler(playlistDir))
		mux.HandleFunc("/api/playlist/add", makePlaylistAddHandler(playlistDir))
		mux.HandleFunc("/api/playlist/remove", makePlaylistRemoveHandler(playlistDir))
		mux.HandleFunc("/api/playlist/reorder", makePlaylistReorderHandler(playlistDir))
		mux.HandleFunc("/api/playlist/check", makePlaylistCheckHandler(playlistDir))

		// Metadata refresh endpoints
		mux.HandleFunc("/api/metadata/refresh", makeMetadataRefreshHandler(database, q2Dir, ffmpegMgr))
		mux.HandleFunc("/api/metadata/status", makeMetadataStatusHandler())
		mux.HandleFunc("/api/metadata/queue", makeMetadataQueueRemoveHandler())
		mux.HandleFunc("/api/metadata/queue/prioritize", makeMetadataQueuePrioritizeHandler())
		mux.HandleFunc("/api/metadata/cancel", makeMetadataCancelHandler())

		// Wrapper to set cast manager base URL from first request
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if castMgr.IsConnected() || r.URL.Path == "/api/cast/connect" {
				// Set base URL from the request if not already set
				scheme := "http"
				if r.TLS != nil {
					scheme = "https"
				}
				baseURL := fmt.Sprintf("%s://%s", scheme, r.Host)
				castMgr.SetBaseURL(baseURL)
			}
			mux.ServeHTTP(w, r)
		})

		addr := fmt.Sprintf(":%d", *port)
		server := &http.Server{
			Addr:    addr,
			Handler: handler,
		}

		// Handle shutdown signals
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

		// Start server in goroutine
		go func() {
			if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				fmt.Fprintln(os.Stderr, "Server error:", err)
				os.Exit(1)
			}
		}()

		fmt.Printf("Listening on port %s\n", addr)

		// Wait for shutdown signal
		<-sigChan
		fmt.Println("\nShutting down...")

		// Shutdown HTTP server with timeout
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "Server shutdown error:", err)
		}

		fmt.Println("Shutdown complete")

	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", os.Args[1])
		flag.Usage()
		os.Exit(2)

	}

}
