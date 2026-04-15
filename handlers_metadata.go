package main

import (
	"encoding/json"
	"fmt"
	"net/http"

	"jukel.org/q2/db"
	"jukel.org/q2/ffmpeg"
)

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
