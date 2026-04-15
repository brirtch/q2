package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"jukel.org/q2/db"
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
	metadataRefreshQueue  []string           // Queue of paths waiting to be refreshed
	metadataRefreshCancel context.CancelFunc // Function to cancel current scan
)

// Inbox processing state
var (
	inboxMu    sync.RWMutex
	inboxFiles []InboxFileStatus
)

// errScanCancelled is returned when a metadata scan is cancelled
var errScanCancelled = errors.New("scan cancelled")

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
		result := database.Write(`
			UPDATE files SET
				filename = ?, extension = ?, mediatype = ?,
				size = ?, modified_at = ?, indexed_at = CURRENT_TIMESTAMP
			WHERE id = ?`,
			filename, ext, mediaType, info.Size(), info.ModTime(), existingID)
		if result.Err != nil {
			return 0, result.Err
		}
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

// enrichEntriesWithMetadata adds metadata to file entries using a single batch query.
func enrichEntriesWithMetadata(database *db.DB, dirPath string, entries []FileEntry) {
	// Assign media types and build normalised-path → entry index map.
	pathToIdx := make(map[string]int, len(entries))
	var paths []string
	for i := range entries {
		entry := &entries[i]
		if entry.Type == "dir" {
			continue
		}
		fullPath := filepath.Join(dirPath, entry.Name)
		if isImageFile(fullPath) {
			entry.MediaType = "image"
		} else if isAudioFile(fullPath) {
			entry.MediaType = "audio"
		} else if isVideoFile(fullPath) {
			entry.MediaType = "video"
		}
		norm := normalizePath(fullPath)
		pathToIdx[norm] = i
		paths = append(paths, norm)
	}

	if len(paths) == 0 {
		return
	}

	// Build a single IN-clause query for all file paths.
	placeholders := make([]string, len(paths))
	args := make([]interface{}, len(paths))
	for i, p := range paths {
		placeholders[i] = "?"
		args[i] = p
	}
	query := `
		SELECT f.path, f.thumbnail_small_path, f.thumbnail_large_path,
		       am.title, am.artist, am.album, am.duration_seconds
		FROM files f
		LEFT JOIN audio_metadata am ON f.id = am.file_id
		WHERE f.path IN (` + strings.Join(placeholders, ",") + `)`

	rows, err := database.Query(query, args...)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var normPath string
		var thumbSmall, thumbLarge, title, artist, album *string
		var duration *int
		if err := rows.Scan(&normPath, &thumbSmall, &thumbLarge, &title, &artist, &album, &duration); err != nil {
			continue
		}
		i, ok := pathToIdx[normPath]
		if !ok {
			continue
		}
		entry := &entries[i]
		fullPath := filepath.Join(dirPath, entry.Name)
		if thumbSmall != nil && *thumbSmall != "" {
			entry.ThumbnailSmall = "/api/thumbnail?path=" + url.QueryEscape(fullPath) + "&size=small"
		}
		if thumbLarge != nil && *thumbLarge != "" {
			entry.ThumbnailLarge = "/api/thumbnail?path=" + url.QueryEscape(fullPath) + "&size=large"
		}
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

