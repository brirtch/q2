package main

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"jukel.org/q2/cast"
)

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

