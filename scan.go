package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"jukel.org/q2/ffmpeg"
	"jukel.org/q2/media"
	"jukel.org/q2/db"
)

// It processes the given path, then drains the queue iteratively (no recursion).
func refreshMetadata(database *db.DB, rootPath string, q2Dir string, ffmpegMgr *ffmpeg.Manager) {
	for {
		runOneRefresh(database, rootPath, q2Dir, ffmpegMgr)

		metadataRefreshMu.Lock()
		if len(metadataRefreshQueue) == 0 {
			metadataRefreshActive = false
			metadataRefreshMu.Unlock()
			return
		}
		rootPath = metadataRefreshQueue[0]
		metadataRefreshQueue = metadataRefreshQueue[1:]
		metadataRefreshMu.Unlock()
	}
}

// runOneRefresh performs a single metadata scan for rootPath.
func runOneRefresh(database *db.DB, rootPath string, q2Dir string, ffmpegMgr *ffmpeg.Manager) {
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
		metadataRefreshMu.Lock()
		metadataRefreshCancel = nil
		metadataRefreshMu.Unlock()
	}()

	// Cache folder list once — avoids a full table scan per file during the walk.
	type folderRecord struct {
		id     int64
		prefix string // normalised path + separator
		exact  string // normalised path without separator
	}
	var cachedFolders []folderRecord
	if rows, err := database.Query("SELECT id, path FROM folders ORDER BY LENGTH(path) DESC"); err == nil {
		for rows.Next() {
			var id int64
			var p string
			if rows.Scan(&id, &p) == nil {
				norm := normalizePath(p)
				prefix := norm
				if !strings.HasSuffix(prefix, string(filepath.Separator)) {
					prefix += string(filepath.Separator)
				}
				cachedFolders = append(cachedFolders, folderRecord{id: id, prefix: prefix, exact: norm})
			}
		}
		rows.Close()
	}

	folderIDForPath := func(filePath string) (int64, bool) {
		norm := normalizePath(filePath)
		for _, f := range cachedFolders {
			if norm == f.exact || strings.HasPrefix(norm, f.prefix) {
				return f.id, true
			}
		}
		return 0, false
	}

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

		// Get folder ID for this file (from cache — no DB query per file)
		folderID, ok := folderIDForPath(path)
		if !ok {
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
				// Get duration via ffprobe (tag library doesn't provide it)
				if ffmpegMgr != nil {
					if dur, err := ffmpegMgr.GetVideoDuration(ctx, path); err == nil {
						d := int(dur)
						meta.DurationSeconds = &d
					}
				}
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
