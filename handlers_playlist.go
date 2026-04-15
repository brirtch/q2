package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"jukel.org/q2/db"
)

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

// enrichPlaylistSongs fills Artist and Album from the audio_metadata table.
func enrichPlaylistSongs(database *db.DB, songs []PlaylistSong) {
	if len(songs) == 0 {
		return
	}
	placeholders := make([]string, len(songs))
	args := make([]interface{}, len(songs))
	pathToIdx := make(map[string]int, len(songs))
	for i, s := range songs {
		norm := normalizePath(s.Path)
		placeholders[i] = "?"
		args[i] = norm
		pathToIdx[norm] = i
	}
	query := `SELECT f.path, COALESCE(am.artist,''), COALESCE(am.album,'')
		FROM files f
		LEFT JOIN audio_metadata am ON f.id = am.file_id
		WHERE f.path IN (` + strings.Join(placeholders, ",") + `)`
	rows, err := database.Query(query, args...)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var p, artist, album string
		if rows.Scan(&p, &artist, &album) == nil {
			if i, ok := pathToIdx[p]; ok {
				songs[i].Artist = artist
				songs[i].Album = album
			}
		}
	}
}

// makePlaylistHandler creates a handler for /api/playlist (CRUD operations).
func makePlaylistHandler(playlistDir string, database *db.DB) http.HandlerFunc {
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
			if !strings.HasPrefix(strings.ToLower(absPath), strings.ToLower(absPlaylistDir)+string(filepath.Separator)) {
				writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "path outside playlists directory"})
				return
			}

			songs, err := parseM3U8(path)
			if err != nil {
				writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "playlist not found"})
				return
			}

			// Enrich with artist/album from DB
			enrichPlaylistSongs(database, songs)

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
			if !strings.HasPrefix(strings.ToLower(absPath), strings.ToLower(absPlaylistDir)+string(filepath.Separator)) {
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

