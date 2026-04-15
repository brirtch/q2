package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"jukel.org/q2/db"
	"jukel.org/q2/ffmpeg"
	"jukel.org/q2/media"
)

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
			enrichEntriesWithMetadata(database, path, entries)
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

