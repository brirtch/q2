package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"jukel.org/q2/db"
)

func makeMusicArtistsHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}
		rows, err := database.Query(`
			SELECT COALESCE(am.artist, '') as artist, COUNT(*) as song_count
			FROM audio_metadata am
			JOIN files f ON f.id = am.file_id
			WHERE am.artist IS NOT NULL AND am.artist != ''
			GROUP BY am.artist
			ORDER BY am.artist COLLATE NOCASE`)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "database error"})
			return
		}
		defer rows.Close()

		type ArtistEntry struct {
			Name      string `json:"name"`
			SongCount int    `json:"song_count"`
		}
		var artists []ArtistEntry
		for rows.Next() {
			var a ArtistEntry
			if err := rows.Scan(&a.Name, &a.SongCount); err == nil {
				artists = append(artists, a)
			}
		}
		if artists == nil {
			artists = []ArtistEntry{}
		}
		writeJSON(w, http.StatusOK, artists)
	}
}

// makeMusicAlbumsHandler returns all distinct albums from audio_metadata.
func makeMusicAlbumsHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}
		artist := r.URL.Query().Get("artist")
		var rows interface{ Next() bool; Scan(...interface{}) error; Close() error }
		var err error
		if artist != "" {
			rows2, err2 := database.Query(`
				SELECT COALESCE(am.album, '') as album, COALESCE(am.artist, '') as artist,
				       COUNT(*) as song_count, COALESCE(am.year, 0) as year
				FROM audio_metadata am
				JOIN files f ON f.id = am.file_id
				WHERE am.album IS NOT NULL AND am.album != '' AND am.artist = ?
				GROUP BY am.album, am.artist
				ORDER BY am.year DESC, am.album COLLATE NOCASE`, artist)
			rows = rows2
			err = err2
		} else {
			rows2, err2 := database.Query(`
				SELECT COALESCE(am.album, '') as album, COALESCE(am.artist, '') as artist,
				       COUNT(*) as song_count, COALESCE(am.year, 0) as year
				FROM audio_metadata am
				JOIN files f ON f.id = am.file_id
				WHERE am.album IS NOT NULL AND am.album != ''
				GROUP BY am.album, am.artist
				ORDER BY am.album COLLATE NOCASE`)
			rows = rows2
			err = err2
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "database error"})
			return
		}
		defer rows.Close()

		type AlbumEntry struct {
			Name      string `json:"name"`
			Artist    string `json:"artist"`
			SongCount int    `json:"song_count"`
			Year      int    `json:"year"`
		}
		var albums []AlbumEntry
		for rows.Next() {
			var a AlbumEntry
			if err := rows.Scan(&a.Name, &a.Artist, &a.SongCount, &a.Year); err == nil {
				albums = append(albums, a)
			}
		}
		if albums == nil {
			albums = []AlbumEntry{}
		}
		writeJSON(w, http.StatusOK, albums)
	}
}

// makeMusicGenresHandler returns all distinct genres from audio_metadata.
func makeMusicGenresHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}
		rows, err := database.Query(`
			SELECT COALESCE(am.genre, '') as genre, COUNT(*) as song_count
			FROM audio_metadata am
			JOIN files f ON f.id = am.file_id
			WHERE am.genre IS NOT NULL AND am.genre != ''
			GROUP BY am.genre
			ORDER BY am.genre COLLATE NOCASE`)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "database error"})
			return
		}
		defer rows.Close()

		type GenreEntry struct {
			Name      string `json:"name"`
			SongCount int    `json:"song_count"`
		}
		var genres []GenreEntry
		for rows.Next() {
			var g GenreEntry
			if err := rows.Scan(&g.Name, &g.SongCount); err == nil {
				genres = append(genres, g)
			}
		}
		if genres == nil {
			genres = []GenreEntry{}
		}
		writeJSON(w, http.StatusOK, genres)
	}
}

// makeMusicSongsHandler returns songs, optionally filtered by artist/album/genre.
func makeMusicSongsHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		artist := r.URL.Query().Get("artist")
		album := r.URL.Query().Get("album")
		genre := r.URL.Query().Get("genre")

		query := `
			SELECT f.path, f.filename, COALESCE(am.title, f.filename) as title,
			       COALESCE(am.artist, '') as artist, COALESCE(am.album, '') as album,
			       COALESCE(am.genre, '') as genre, COALESCE(am.track_number, 0) as track_number,
			       COALESCE(am.year, 0) as year, COALESCE(am.duration_seconds, 0) as duration
			FROM audio_metadata am
			JOIN files f ON f.id = am.file_id`

		var conditions []string
		var args []interface{}
		if artist != "" {
			conditions = append(conditions, "am.artist = ?")
			args = append(args, artist)
		}
		if album != "" {
			conditions = append(conditions, "am.album = ?")
			args = append(args, album)
		}
		if genre != "" {
			conditions = append(conditions, "am.genre = ?")
			args = append(args, genre)
		}
		if len(conditions) > 0 {
			query += " WHERE " + strings.Join(conditions, " AND ")
		}
		query += " ORDER BY am.artist COLLATE NOCASE, am.album COLLATE NOCASE, am.track_number, am.title COLLATE NOCASE"

		rows, err := database.Query(query, args...)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "database error"})
			return
		}
		defer rows.Close()

		type SongEntry struct {
			Path        string `json:"path"`
			Filename    string `json:"filename"`
			Title       string `json:"title"`
			Artist      string `json:"artist"`
			Album       string `json:"album"`
			Genre       string `json:"genre"`
			TrackNumber int    `json:"track_number"`
			Year        int    `json:"year"`
			Duration    int    `json:"duration"`
		}
		var songs []SongEntry
		for rows.Next() {
			var s SongEntry
			if err := rows.Scan(&s.Path, &s.Filename, &s.Title, &s.Artist, &s.Album, &s.Genre, &s.TrackNumber, &s.Year, &s.Duration); err == nil {
				songs = append(songs, s)
			}
		}
		if songs == nil {
			songs = []SongEntry{}
		}
		writeJSON(w, http.StatusOK, songs)
	}
}

// makeFavouritesHandler handles GET /api/favourites?type=<type> and POST /api/favourites/toggle.
func makeFavouritesHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		favType := r.URL.Query().Get("type")
		switch r.Method {
		case http.MethodGet:
			// Return all favourite keys for the given type
			rows, err := database.Query(`SELECT key FROM favourites WHERE type = ? ORDER BY created_at DESC`, favType)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "database error"})
				return
			}
			defer rows.Close()
			var keys []string
			for rows.Next() {
				var k string
				if rows.Scan(&k) == nil {
					keys = append(keys, k)
				}
			}
			if keys == nil {
				keys = []string{}
			}
			writeJSON(w, http.StatusOK, map[string][]string{"keys": keys})

		case http.MethodPost:
			// Toggle a favourite; body: {"type":"artist","key":"The Beatles"}
			var req struct {
				Type string `json:"type"`
				Key  string `json:"key"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Type == "" || req.Key == "" {
				writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "type and key required"})
				return
			}
			// Try to delete; if nothing deleted, insert
			res := database.Write(`DELETE FROM favourites WHERE type = ? AND key = ?`, req.Type, req.Key)
			if res.Err != nil {
				writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "database error"})
				return
			}
			favourited := false
			if res.RowsAffected == 0 {
				database.Write(`INSERT INTO favourites (type, key) VALUES (?, ?)`, req.Type, req.Key)
				favourited = true
			}
			writeJSON(w, http.StatusOK, map[string]bool{"favourited": favourited})

		default:
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
		}
	}
}

// makeRecordPlayHandler records a song play in play_history.
func makeRecordPlayHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}
		filePath := r.URL.Query().Get("path")
		if filePath == "" {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "path required"})
			return
		}
		var fileID int64
		if err := database.QueryRow(`SELECT id FROM files WHERE path = ?`, filePath).Scan(&fileID); err != nil {
			writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "file not found"})
			return
		}
		database.Write(`INSERT INTO play_history (file_id) VALUES (?)`, fileID)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// makeTopSongsHandler returns the most played songs in the last 3 months.
func makeTopSongsHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}
		rows, err := database.Query(`
			SELECT f.path, f.filename,
			       COALESCE(am.title, f.filename) as title,
			       COALESCE(am.artist, '') as artist,
			       COALESCE(am.album, '') as album,
			       COALESCE(am.duration_seconds, 0) as duration,
			       COUNT(ph.id) as play_count
			FROM play_history ph
			JOIN files f ON f.id = ph.file_id
			LEFT JOIN audio_metadata am ON am.file_id = ph.file_id
			WHERE ph.played_at >= datetime('now', '-3 months')
			GROUP BY ph.file_id
			ORDER BY play_count DESC
			LIMIT 50
		`)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "database error"})
			return
		}
		defer rows.Close()

		type TopSong struct {
			Path      string `json:"path"`
			Filename  string `json:"filename"`
			Title     string `json:"title"`
			Artist    string `json:"artist"`
			Album     string `json:"album"`
			Duration  int    `json:"duration"`
			PlayCount int    `json:"play_count"`
		}
		var songs []TopSong
		for rows.Next() {
			var s TopSong
			if err := rows.Scan(&s.Path, &s.Filename, &s.Title, &s.Artist, &s.Album, &s.Duration, &s.PlayCount); err == nil {
				songs = append(songs, s)
			}
		}
		if songs == nil {
			songs = []TopSong{}
		}
		writeJSON(w, http.StatusOK, songs)
	}
}

// makeLyricsHandler handles GET /api/lyrics?path=<filepath>.
// It checks the lyrics table for cached results, and if not found, fetches from lrclib.net.
func makeLyricsHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		filePath := r.URL.Query().Get("path")
		if filePath == "" {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "path parameter required"})
			return
		}

		// Look up the file in the files table
		var fileID int64
		row := database.QueryRow(`SELECT id FROM files WHERE path = ?`, filePath)
		if err := row.Scan(&fileID); err != nil {
			writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "file not found"})
			return
		}

		// Check if lyrics already exist in DB (only use cache if we actually have content)
		var syncedLyrics, plainLyrics string
		lyricsRow := database.QueryRow(`SELECT COALESCE(synced_lyrics,''), COALESCE(plain_lyrics,'') FROM lyrics WHERE file_id = ?`, fileID)
		if err := lyricsRow.Scan(&syncedLyrics, &plainLyrics); err == nil && (syncedLyrics != "" || plainLyrics != "") {
			writeJSON(w, http.StatusOK, LyricsResponse{SyncedLyrics: syncedLyrics, PlainLyrics: plainLyrics})
			return
		}

		// Get metadata to build lrclib query
		var title, artist, album string
		var durationSeconds float64
		metaRow := database.QueryRow(`
			SELECT COALESCE(am.title,''), COALESCE(am.artist,''), COALESCE(am.album,''), COALESCE(am.duration_seconds,0)
			FROM audio_metadata am
			WHERE am.file_id = ?`, fileID)
		if err := metaRow.Scan(&title, &artist, &album, &durationSeconds); err != nil {
			// No metadata yet — don't cache, allow retry after metadata is scanned
			writeJSON(w, http.StatusOK, LyricsResponse{})
			return
		}

		client := &http.Client{Timeout: 10 * time.Second}

		lrclibFetch := func(lrclibURL string) (string, string, bool) {
			req, err := http.NewRequest(http.MethodGet, lrclibURL, nil)
			if err != nil {
				return "", "", false
			}
			req.Header.Set("User-Agent", "q2-media-manager/1.0 (https://github.com/brirtch/q2)")
			resp, err := client.Do(req)
			if err != nil {
				return "", "", false
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return "", "", false
			}
			var lrclibResp struct {
				SyncedLyrics string `json:"syncedLyrics"`
				PlainLyrics  string `json:"plainLyrics"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&lrclibResp); err != nil {
				return "", "", false
			}
			return lrclibResp.SyncedLyrics, lrclibResp.PlainLyrics, true
		}

		// Try /api/get first (exact match). Include duration only when known.
		getParams := url.Values{
			"artist_name": {artist},
			"track_name":  {title},
			"album_name":  {album},
		}
		if durationSeconds > 0 {
			getParams.Set("duration", strconv.Itoa(int(durationSeconds)))
		}
		synced, plain, ok := lrclibFetch("https://lrclib.net/api/get?" + getParams.Encode())

		// Fall back to /api/search when get fails (no duration match, not in catalogue yet, etc.)
		if !ok || (synced == "" && plain == "") {
			searchParams := url.Values{
				"artist_name": {artist},
				"track_name":  {title},
			}
			type searchResult struct {
				SyncedLyrics string `json:"syncedLyrics"`
				PlainLyrics  string `json:"plainLyrics"`
			}
			req, err := http.NewRequest(http.MethodGet, "https://lrclib.net/api/search?"+searchParams.Encode(), nil)
			if err == nil {
				req.Header.Set("User-Agent", "q2-media-manager/1.0 (https://github.com/brirtch/q2)")
				resp, err := client.Do(req)
				if err == nil && resp.StatusCode == http.StatusOK {
					var results []searchResult
					if json.NewDecoder(resp.Body).Decode(&results) == nil && len(results) > 0 {
						synced = results[0].SyncedLyrics
						plain = results[0].PlainLyrics
						ok = true
					}
					resp.Body.Close()
				}
			}
		}

		if !ok || (synced == "" && plain == "") {
			// Don't cache empty results so we retry next time
			writeJSON(w, http.StatusOK, LyricsResponse{})
			return
		}

		// Cache successful result only
		database.Write(`INSERT INTO lyrics (file_id, synced_lyrics, plain_lyrics) VALUES (?, ?, ?) ON CONFLICT(file_id) DO UPDATE SET synced_lyrics=excluded.synced_lyrics, plain_lyrics=excluded.plain_lyrics, fetched_at=CURRENT_TIMESTAMP`,
			fileID, synced, plain)

		writeJSON(w, http.StatusOK, LyricsResponse{
			SyncedLyrics: synced,
			PlainLyrics:  plain,
		})
	}
}

// musicPageHandler serves the music library page.
