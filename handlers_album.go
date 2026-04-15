package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"

	"jukel.org/q2/db"
)

// makeAlbumsHandler creates a handler for /api/albums (list all albums).
func makeAlbumsHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		rows, err := database.Query(`
			SELECT a.id, a.name, a.cover_path, a.created_at, a.updated_at,
			       (SELECT COUNT(*) FROM album_items WHERE album_id = a.id) as item_count
			FROM albums a
			ORDER BY a.name
		`)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "failed to query albums"})
			return
		}
		defer rows.Close()

		var albums []Album
		for rows.Next() {
			var a Album
			var coverPath, createdAt, updatedAt *string
			if err := rows.Scan(&a.ID, &a.Name, &coverPath, &createdAt, &updatedAt, &a.ItemCount); err != nil {
				continue
			}
			if coverPath != nil {
				a.CoverPath = *coverPath
			}
			if createdAt != nil {
				a.CreatedAt = *createdAt
			}
			if updatedAt != nil {
				a.UpdatedAt = *updatedAt
			}
			albums = append(albums, a)
		}

		if albums == nil {
			albums = []Album{}
		}
		writeJSON(w, http.StatusOK, AlbumsResponse{Albums: albums})
	}
}

// makeAlbumHandler creates a handler for /api/album (CRUD operations).
func makeAlbumHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			// Read album contents
			idStr := r.URL.Query().Get("id")
			if idStr == "" {
				writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "id parameter required"})
				return
			}

			id, err := strconv.ParseInt(idStr, 10, 64)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid id"})
				return
			}

			// Get album info
			var album Album
			var coverPath, createdAt, updatedAt *string
			row := database.QueryRow(`
				SELECT a.id, a.name, a.cover_path, a.created_at, a.updated_at,
				       (SELECT COUNT(*) FROM album_items WHERE album_id = a.id) as item_count
				FROM albums a WHERE a.id = ?`, id)
			if err := row.Scan(&album.ID, &album.Name, &coverPath, &createdAt, &updatedAt, &album.ItemCount); err != nil {
				writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "album not found"})
				return
			}
			if coverPath != nil {
				album.CoverPath = *coverPath
			}
			if createdAt != nil {
				album.CreatedAt = *createdAt
			}
			if updatedAt != nil {
				album.UpdatedAt = *updatedAt
			}

			// Get album items
			rows, err := database.Query(`
				SELECT ai.id, ai.file_id, ai.position, f.path, f.filename,
				       f.thumbnail_small_path, f.thumbnail_large_path
				FROM album_items ai
				JOIN files f ON ai.file_id = f.id
				WHERE ai.album_id = ?
				ORDER BY ai.position`, id)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "failed to query album items"})
				return
			}
			defer rows.Close()

			var items []AlbumItem
			for rows.Next() {
				var item AlbumItem
				var thumbSmall, thumbLarge *string
				if err := rows.Scan(&item.ID, &item.FileID, &item.Position, &item.Path, &item.Filename, &thumbSmall, &thumbLarge); err != nil {
					continue
				}
				if thumbSmall != nil && *thumbSmall != "" {
					item.ThumbnailSmall = "/api/thumbnail?path=" + url.QueryEscape(item.Path) + "&size=small"
				}
				if thumbLarge != nil && *thumbLarge != "" {
					item.ThumbnailLarge = "/api/thumbnail?path=" + url.QueryEscape(item.Path) + "&size=large"
				}
				items = append(items, item)
			}

			if items == nil {
				items = []AlbumItem{}
			}
			writeJSON(w, http.StatusOK, AlbumResponse{Album: album, Items: items})

		case http.MethodPost:
			// Create new album
			var req AlbumCreateRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
				return
			}

			if req.Name == "" {
				writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "name is required"})
				return
			}

			result := database.Write(`INSERT INTO albums (name) VALUES (?)`, req.Name)
			if result.Err != nil {
				writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "failed to create album"})
				return
			}

			writeJSON(w, http.StatusOK, map[string]interface{}{
				"success": true,
				"id":      result.LastInsertID,
				"name":    req.Name,
			})

		case http.MethodDelete:
			// Delete album
			idStr := r.URL.Query().Get("id")
			if idStr == "" {
				writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "id parameter required"})
				return
			}

			id, err := strconv.ParseInt(idStr, 10, 64)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid id"})
				return
			}

			result := database.Write(`DELETE FROM albums WHERE id = ?`, id)
			if result.Err != nil {
				writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "failed to delete album"})
				return
			}

			writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})

		default:
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
		}
	}
}

// makeAlbumAddHandler creates a handler for /api/album/add.
func makeAlbumAddHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		var req AlbumAddRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
			return
		}

		if req.AlbumID == 0 || req.Path == "" {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "album_id and path are required"})
			return
		}

		// Look up file ID from path
		normalizedPath := normalizePath(req.Path)
		var fileID int64
		row := database.QueryRow(`SELECT id FROM files WHERE path = ?`, normalizedPath)
		if err := row.Scan(&fileID); err != nil {
			writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "file not found in database"})
			return
		}

		// Get max position for this album
		var maxPos int
		row = database.QueryRow(`SELECT COALESCE(MAX(position), -1) FROM album_items WHERE album_id = ?`, req.AlbumID)
		row.Scan(&maxPos)

		// Insert the album item
		result := database.Write(`
			INSERT INTO album_items (album_id, file_id, position)
			VALUES (?, ?, ?)
			ON CONFLICT(album_id, file_id) DO NOTHING`,
			req.AlbumID, fileID, maxPos+1)
		if result.Err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "failed to add to album"})
			return
		}

		// Update album's updated_at timestamp
		database.Write(`UPDATE albums SET updated_at = CURRENT_TIMESTAMP WHERE id = ?`, req.AlbumID)

		// Update album cover if not set
		database.Write(`
			UPDATE albums SET cover_path = ?
			WHERE id = ? AND (cover_path IS NULL OR cover_path = '')`,
			normalizedPath, req.AlbumID)

		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
	}
}

// makeAlbumRemoveHandler creates a handler for /api/album/remove.
func makeAlbumRemoveHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		var req AlbumRemoveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
			return
		}

		if req.AlbumID == 0 {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "album_id is required"})
			return
		}

		result := database.Write(`DELETE FROM album_items WHERE id = ? AND album_id = ?`, req.ItemID, req.AlbumID)
		if result.Err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "failed to remove from album"})
			return
		}

		// Update album's updated_at timestamp
		database.Write(`UPDATE albums SET updated_at = CURRENT_TIMESTAMP WHERE id = ?`, req.AlbumID)

		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
	}
}

// makeAlbumReorderHandler creates a handler for /api/album/reorder.
func makeAlbumReorderHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		var req AlbumReorderRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
			return
		}

		if req.AlbumID == 0 {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "album_id is required"})
			return
		}

		// Get all items in current order
		rows, err := database.Query(`
			SELECT id FROM album_items WHERE album_id = ? ORDER BY position`, req.AlbumID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "failed to read album"})
			return
		}

		var itemIDs []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				continue
			}
			itemIDs = append(itemIDs, id)
		}
		rows.Close()

		if req.FromIndex < 0 || req.FromIndex >= len(itemIDs) ||
			req.ToIndex < 0 || req.ToIndex >= len(itemIDs) {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid index"})
			return
		}

		// Reorder
		item := itemIDs[req.FromIndex]
		itemIDs = append(itemIDs[:req.FromIndex], itemIDs[req.FromIndex+1:]...)
		itemIDs = append(itemIDs[:req.ToIndex], append([]int64{item}, itemIDs[req.ToIndex:]...)...)

		// Update positions and album timestamp in a single transaction
		statements := make([]db.Statement, 0, len(itemIDs)+1)
		for i, id := range itemIDs {
			statements = append(statements, db.Statement{
				Query: `UPDATE album_items SET position = ? WHERE id = ?`,
				Args:  []interface{}{i, id},
			})
		}
		statements = append(statements, db.Statement{
			Query: `UPDATE albums SET updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
			Args:  []interface{}{req.AlbumID},
		})
		if err := database.WriteTransaction(statements); err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "failed to save reorder"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
	}
}

// makeAlbumCheckHandler creates a handler for /api/album/check.
func makeAlbumCheckHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		imagePath := r.URL.Query().Get("path")
		if imagePath == "" {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "path parameter required"})
			return
		}

		normalizedPath := normalizePath(imagePath)

		// Get the file ID for this path
		var fileID int64
		row := database.QueryRow(`SELECT id FROM files WHERE path = ?`, normalizedPath)
		if err := row.Scan(&fileID); err != nil {
			// File not in database - just return all albums with contains=false
			rows, err := database.Query(`
				SELECT a.id, a.name, a.cover_path,
				       (SELECT COUNT(*) FROM album_items WHERE album_id = a.id) as item_count
				FROM albums a ORDER BY a.name
			`)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "failed to query albums"})
				return
			}
			defer rows.Close()

			var albums []AlbumWithContains
			for rows.Next() {
				var a Album
				var coverPath *string
				if err := rows.Scan(&a.ID, &a.Name, &coverPath, &a.ItemCount); err != nil {
					continue
				}
				if coverPath != nil {
					a.CoverPath = *coverPath
				}
				albums = append(albums, AlbumWithContains{Album: a, Contains: false})
			}

			if albums == nil {
				albums = []AlbumWithContains{}
			}
			writeJSON(w, http.StatusOK, AlbumCheckResponse{Albums: albums})
			return
		}

		// Get all albums with contains flag
		rows, err := database.Query(`
			SELECT a.id, a.name, a.cover_path,
			       (SELECT COUNT(*) FROM album_items WHERE album_id = a.id) as item_count,
			       EXISTS(SELECT 1 FROM album_items WHERE album_id = a.id AND file_id = ?) as contains
			FROM albums a ORDER BY a.name
		`, fileID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "failed to query albums"})
			return
		}
		defer rows.Close()

		var albums []AlbumWithContains
		for rows.Next() {
			var a Album
			var coverPath *string
			var contains bool
			if err := rows.Scan(&a.ID, &a.Name, &coverPath, &a.ItemCount, &contains); err != nil {
				continue
			}
			if coverPath != nil {
				a.CoverPath = *coverPath
			}
			albums = append(albums, AlbumWithContains{Album: a, Contains: contains})
		}

		if albums == nil {
			albums = []AlbumWithContains{}
		}
		writeJSON(w, http.StatusOK, AlbumCheckResponse{Albums: albums})
	}
}


