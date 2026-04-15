package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"

	"jukel.org/q2/db"
	"jukel.org/q2/ffmpeg"
	"jukel.org/q2/media"
)

func makeSettingsGetHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		rows, err := database.Query("SELECT key, value FROM settings")
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "database error"})
			return
		}
		defer rows.Close()

		settings := make(map[string]string)
		for rows.Next() {
			var key, value string
			if err := rows.Scan(&key, &value); err == nil {
				settings[key] = value
			}
		}

		writeJSON(w, http.StatusOK, settings)
	}
}

// makeSettingsPostHandler creates a handler for POST /api/settings.
func makeSettingsPostHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		var settings map[string]string
		if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid JSON"})
			return
		}

		for key, value := range settings {
			result := database.Write(
				"INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
				key, value,
			)
			if result.Err != nil {
				writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "database error"})
				return
			}
		}

		writeJSON(w, http.StatusOK, map[string]bool{"success": true})
	}
}

// makeFolderAddHandler creates a handler for POST /api/folders/add.
func makeFolderAddHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		var req struct {
			Path string `json:"path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid JSON"})
			return
		}

		if req.Path == "" {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "path is required"})
			return
		}

		cleaned, ok := cleanPath(req.Path)
		if !ok {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid path"})
			return
		}

		info, err := os.Stat(cleaned)
		if err != nil {
			if os.IsNotExist(err) {
				writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "folder does not exist"})
				return
			}
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "cannot access folder"})
			return
		}
		if !info.IsDir() {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "path is not a directory"})
			return
		}

		normalizedPath := normalizePath(cleaned)
		result := database.Write("INSERT OR IGNORE INTO folders (path) VALUES (?)", normalizedPath)
		if result.Err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "database error"})
			return
		}

		msg := "added"
		if result.RowsAffected == 0 {
			msg = "already exists"
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": msg, "path": normalizedPath})
	}
}

// makeFolderRemoveHandler creates a handler for POST /api/folders/remove.
func makeFolderRemoveHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		var req struct {
			Path string `json:"path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid JSON"})
			return
		}

		if req.Path == "" {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "path is required"})
			return
		}

		normalizedPath := normalizePath(req.Path)
		result := database.Write("DELETE FROM folders WHERE path = ?", normalizedPath)
		if result.Err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "database error"})
			return
		}

		if result.RowsAffected == 0 {
			writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "folder not found"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]bool{"success": true})
	}
}

// makeInboxUploadHandler creates a handler for POST /api/inbox/upload.
func makeInboxUploadHandler(database *db.DB, q2Dir string, ffmpegMgr *ffmpeg.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		// Get audio destination from settings
		var audioDest string
		row := database.QueryRow("SELECT value FROM settings WHERE key = 'audio_destination'")
		if err := row.Scan(&audioDest); err != nil || audioDest == "" {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "audio destination not configured - set it in Settings"})
			return
		}

		// Parse multipart form (100MB max)
		if err := r.ParseMultipartForm(100 << 20); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "failed to parse upload"})
			return
		}

		files := r.MultipartForm.File["files"]
		if len(files) == 0 {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "no files uploaded"})
			return
		}

		// Initialize status for all files
		batchStart := 0
		inboxMu.Lock()
		batchStart = len(inboxFiles)
		for _, fh := range files {
			inboxFiles = append(inboxFiles, InboxFileStatus{
				Name:   fh.Filename,
				Status: "pending",
			})
		}
		inboxMu.Unlock()

		// Process files in background
		go func() {
			ctx := context.Background()
			for i, fh := range files {
				idx := batchStart + i

				inboxMu.Lock()
				inboxFiles[idx].Status = "processing"
				inboxMu.Unlock()

				err := processInboxFile(ctx, fh, audioDest, database, q2Dir, ffmpegMgr)

				inboxMu.Lock()
				if err != nil {
					inboxFiles[idx].Status = "error"
					inboxFiles[idx].Error = err.Error()
				} else {
					inboxFiles[idx].Status = "done"
				}
				inboxMu.Unlock()
			}
		}()

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"count":   len(files),
			"offset":  batchStart,
		})
	}
}

// processInboxFile handles a single uploaded file: extract metadata, copy to artist/album folder, populate DB.
func processInboxFile(ctx context.Context, fh *multipart.FileHeader, audioDest string, database *db.DB, q2Dir string, ffmpegMgr *ffmpeg.Manager) error {
	// Open the uploaded file
	src, err := fh.Open()
	if err != nil {
		return fmt.Errorf("failed to open uploaded file: %w", err)
	}
	defer src.Close()

	// Sanitise the filename: strip any directory components to prevent path traversal.
	safeFilename := filepath.Base(fh.Filename)
	if safeFilename == "." || safeFilename == string(filepath.Separator) {
		return fmt.Errorf("invalid filename")
	}

	// Write to a temp file so we can extract metadata
	tmpDir := filepath.Join(q2Dir, "inbox_tmp")
	os.MkdirAll(tmpDir, 0755)
	tmpFile := filepath.Join(tmpDir, safeFilename)
	dst, err := os.Create(tmpFile)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		os.Remove(tmpFile)
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	dst.Close()

	// Extract metadata
	meta, err := media.ExtractAudioMetadata(tmpFile)
	if err != nil {
		meta = &media.AudioMetadata{}
	}

	// Get duration via ffprobe
	if ffmpegMgr != nil {
		if dur, err := ffmpegMgr.GetVideoDuration(ctx, tmpFile); err == nil {
			d := int(dur)
			meta.DurationSeconds = &d
		}
	}

	// Determine artist and album folder names
	artist := "Unknown Artist"
	album := "Unknown Album"
	if meta.Artist != nil && *meta.Artist != "" {
		artist = sanitizeFolderName(*meta.Artist)
	}
	if meta.Album != nil && *meta.Album != "" {
		album = sanitizeFolderName(*meta.Album)
	}

	// Create destination path: audioDest/artist/album/filename
	destDir := filepath.Join(audioDest, artist, album)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	destPath := filepath.Join(destDir, safeFilename)

	// Check if file already exists
	if _, err := os.Stat(destPath); err == nil {
		os.Remove(tmpFile)
		return fmt.Errorf("file already exists: %s", destPath)
	}

	// Move temp file to destination
	if err := moveFile(tmpFile, destPath); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("failed to move file: %w", err)
	}

	// Update inbox status with destination
	inboxMu.Lock()
	for i := range inboxFiles {
		if inboxFiles[i].Name == safeFilename && inboxFiles[i].Status == "processing" {
			inboxFiles[i].Dest = filepath.Join(artist, album, safeFilename)
			break
		}
	}
	inboxMu.Unlock()

	// Check if the destination is within a monitored folder - if so, add to DB
	folders, err := getMonitoredFolders(database)
	if err == nil {
		if root := isPathWithinRoots(destPath, folders); root != "" {
			info, err := os.Stat(destPath)
			if err == nil {
				folderID, err := getFolderIDForPath(database, destPath)
				if err == nil {
					fileID, err := upsertFile(database, folderID, destPath, info)
					if err == nil {
						media.SaveAudioMetadata(database, fileID, meta)
					}
				}
			}
		}
	}

	return nil
}

// makeInboxStatusHandler creates a handler for GET /api/inbox/status.
func makeInboxStatusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		inboxMu.RLock()
		files := make([]InboxFileStatus, len(inboxFiles))
		copy(files, inboxFiles)
		inboxMu.RUnlock()

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"files": files,
		})
	}
}

// makeInboxClearHandler creates a handler for POST /api/inbox/clear.
func makeInboxClearHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		inboxMu.Lock()
		inboxFiles = nil
		inboxMu.Unlock()

		writeJSON(w, http.StatusOK, map[string]bool{"success": true})
	}
}

// settingsPageHandler serves the settings page.
