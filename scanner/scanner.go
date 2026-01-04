package scanner

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"jukel.org/q2/db"
)

// Media type constants
const (
	MediaTypeImage = "IMG"
	MediaTypeVideo = "VID"
	MediaTypeAudio = "AUD"
)

// Image file extensions
var imageExtensions = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true,
	".bmp": true, ".webp": true, ".tiff": true, ".tif": true,
	".heic": true, ".heif": true, ".raw": true, ".cr2": true, ".nef": true,
}

// Video file extensions
var videoExtensions = map[string]bool{
	".mp4": true, ".avi": true, ".mkv": true, ".mov": true,
	".wmv": true, ".flv": true, ".webm": true, ".m4v": true,
}

// Audio file extensions
var audioExtensions = map[string]bool{
	".mp3": true, ".wav": true, ".flac": true, ".aac": true,
	".ogg": true, ".wma": true, ".m4a": true,
}

// GetMediaType returns the media type for a file extension.
// Returns nil for unknown/unclassified file types.
func GetMediaType(extension string) *string {
	ext := strings.ToLower(extension)
	if imageExtensions[ext] {
		mt := MediaTypeImage
		return &mt
	}
	if videoExtensions[ext] {
		mt := MediaTypeVideo
		return &mt
	}
	if audioExtensions[ext] {
		mt := MediaTypeAudio
		return &mt
	}
	return nil
}

// normalizePath applies platform-specific path normalization.
func normalizePath(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}

// ScanResult holds the results of a scan operation.
type ScanResult struct {
	FilesAdded   int
	FilesUpdated int
	FilesRemoved int
	Errors       []error
}

// ScanFolder recursively scans a folder and indexes all files.
// folderID is the ID of the parent folder in the folders table.
func ScanFolder(database *db.DB, folderPath string, folderID int64) (*ScanResult, error) {
	result := &ScanResult{}

	// Track all file paths we encounter during scan
	scannedPaths := make(map[string]bool)

	err := filepath.Walk(folderPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("error accessing %s: %w", path, err))
			return nil // Continue walking
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		normalizedPath := normalizePath(path)
		scannedPaths[normalizedPath] = true

		added, updated, scanErr := scanFile(database, path, info, folderID)
		if scanErr != nil {
			result.Errors = append(result.Errors, fmt.Errorf("error scanning %s: %w", path, scanErr))
			return nil // Continue walking
		}

		if added {
			result.FilesAdded++
		} else if updated {
			result.FilesUpdated++
		}

		return nil
	})

	if err != nil {
		return result, fmt.Errorf("error walking folder: %w", err)
	}

	// Remove files that no longer exist
	removed, removeErr := removeDeletedFiles(database, folderID, scannedPaths)
	if removeErr != nil {
		result.Errors = append(result.Errors, fmt.Errorf("error removing deleted files: %w", removeErr))
	}
	result.FilesRemoved = removed

	return result, nil
}

// scanFile indexes a single file, returning whether it was added or updated.
func scanFile(database *db.DB, path string, info os.FileInfo, folderID int64) (added bool, updated bool, err error) {
	normalizedPath := normalizePath(path)
	filename := info.Name()
	extension := strings.ToLower(filepath.Ext(filename))
	mediaType := GetMediaType(extension)
	size := info.Size()
	modTime := info.ModTime()

	// Get created time (platform-specific, use mod time as fallback)
	createdTime := modTime

	// Check if file already exists in database
	var existingID int64
	var existingModTime time.Time
	row := database.QueryRow("SELECT id, modified_at FROM files WHERE path = ?", normalizedPath)
	scanErr := row.Scan(&existingID, &existingModTime)

	if scanErr == nil {
		// File exists - check if it needs updating
		if !modTime.Equal(existingModTime) {
			result := database.Write(`
				UPDATE files SET
					filename = ?,
					extension = ?,
					mediatype = ?,
					size = ?,
					modified_at = ?,
					indexed_at = CURRENT_TIMESTAMP
				WHERE id = ?
			`, filename, extension, mediaType, size, modTime, existingID)
			if result.Err != nil {
				return false, false, result.Err
			}
			return false, true, nil
		}
		// File unchanged
		return false, false, nil
	}

	// File doesn't exist - insert it
	result := database.Write(`
		INSERT INTO files (folder_id, path, filename, extension, mediatype, size, created_at, modified_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, folderID, normalizedPath, filename, extension, mediaType, size, createdTime, modTime)

	if result.Err != nil {
		return false, false, result.Err
	}

	return true, false, nil
}

// removeDeletedFiles removes database entries for files that no longer exist on disk.
func removeDeletedFiles(database *db.DB, folderID int64, existingPaths map[string]bool) (int, error) {
	// Get all files for this folder from the database
	rows, err := database.Query("SELECT id, path FROM files WHERE folder_id = ?", folderID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var idsToRemove []int64
	for rows.Next() {
		var id int64
		var path string
		if err := rows.Scan(&id, &path); err != nil {
			return 0, err
		}

		// If the path wasn't found during scan, mark for removal
		if !existingPaths[path] {
			idsToRemove = append(idsToRemove, id)
		}
	}

	if err := rows.Err(); err != nil {
		return 0, err
	}

	// Remove the files that no longer exist
	for _, id := range idsToRemove {
		result := database.Write("DELETE FROM files WHERE id = ?", id)
		if result.Err != nil {
			return 0, result.Err
		}
	}

	return len(idsToRemove), nil
}

// GetFolderID retrieves the folder ID for a given path.
// Returns the folder ID if found, or an error if not found or on database error.
func GetFolderID(database *db.DB, folderPath string) (int64, error) {
	normalizedPath := normalizePath(folderPath)

	var id int64
	row := database.QueryRow("SELECT id FROM folders WHERE path = ?", normalizedPath)
	err := row.Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("folder not found: %s", folderPath)
	}

	return id, nil
}

// IsSubfolderOf checks if childPath is equal to or a subfolder of parentPath.
func IsSubfolderOf(childPath, parentPath string) bool {
	childNorm := normalizePath(childPath)
	parentNorm := normalizePath(parentPath)

	if childNorm == parentNorm {
		return true
	}

	// Ensure parent path ends with separator for proper prefix matching
	if !strings.HasSuffix(parentNorm, string(filepath.Separator)) {
		parentNorm += string(filepath.Separator)
	}

	return strings.HasPrefix(childNorm, parentNorm)
}

// FindParentFolder finds the monitored folder that contains the given path.
// Returns the folder path and ID if found, or an error if the path is not
// within any monitored folder.
func FindParentFolder(database *db.DB, path string) (string, int64, error) {
	normalizedPath := normalizePath(path)

	rows, err := database.Query("SELECT id, path FROM folders")
	if err != nil {
		return "", 0, err
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		var folderPath string
		if err := rows.Scan(&id, &folderPath); err != nil {
			return "", 0, err
		}

		if IsSubfolderOf(normalizedPath, folderPath) {
			return folderPath, id, nil
		}
	}

	if err := rows.Err(); err != nil {
		return "", 0, err
	}

	return "", 0, fmt.Errorf("path is not within any monitored folder: %s", path)
}

// QueueScan adds a folder to the scan queue.
func QueueScan(database *db.DB, path string) error {
	normalizedPath := normalizePath(path)

	result := database.Write(`
		INSERT OR REPLACE INTO scan_queue (path, requested_at, started_at, completed_at)
		VALUES (?, CURRENT_TIMESTAMP, NULL, NULL)
	`, normalizedPath)

	return result.Err
}

// GetPendingScans returns paths that are queued for scanning but not yet completed.
func GetPendingScans(database *db.DB) ([]string, error) {
	rows, err := database.Query(`
		SELECT path FROM scan_queue
		WHERE completed_at IS NULL
		ORDER BY requested_at
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var paths []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}

	return paths, rows.Err()
}

// MarkScanStarted marks a scan as started.
func MarkScanStarted(database *db.DB, path string) error {
	normalizedPath := normalizePath(path)
	result := database.Write(`
		UPDATE scan_queue SET started_at = CURRENT_TIMESTAMP WHERE path = ?
	`, normalizedPath)
	return result.Err
}

// MarkScanCompleted marks a scan as completed.
func MarkScanCompleted(database *db.DB, path string) error {
	normalizedPath := normalizePath(path)
	result := database.Write(`
		UPDATE scan_queue SET completed_at = CURRENT_TIMESTAMP WHERE path = ?
	`, normalizedPath)
	return result.Err
}

// RemoveCompletedScan removes a completed scan from the queue.
func RemoveCompletedScan(database *db.DB, path string) error {
	normalizedPath := normalizePath(path)
	result := database.Write(`DELETE FROM scan_queue WHERE path = ?`, normalizedPath)
	return result.Err
}
