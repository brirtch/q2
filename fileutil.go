package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// audioContentTypes maps audio file extensions to their MIME types.
var audioContentTypes = map[string]string{
	".mp3":  "audio/mpeg",
	".wav":  "audio/wav",
	".flac": "audio/flac",
	".aac":  "audio/aac",
	".ogg":  "audio/ogg",
	".wma":  "audio/x-ms-wma",
	".m4a":  "audio/mp4",
}

// isAudioFile checks if the file extension is a supported audio format.
func isAudioFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	_, ok := audioContentTypes[ext]
	return ok
}

// imageContentTypes maps image file extensions to their MIME types.
var imageContentTypes = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".gif":  "image/gif",
	".webp": "image/webp",
	".bmp":  "image/bmp",
	".svg":  "image/svg+xml",
	".ico":  "image/x-icon",
}

// isImageFile checks if the file extension is a supported image format.
func isImageFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	_, ok := imageContentTypes[ext]
	return ok
}

// videoContentTypes maps video file extensions to their MIME types.
var videoContentTypes = map[string]string{
	".mp4":  "video/mp4",
	".webm": "video/webm",
	".ogv":  "video/ogg",
	".mov":  "video/quicktime",
	".avi":  "video/x-msvideo",
	".mkv":  "video/x-matroska",
	".m4v":  "video/mp4",
}

// isVideoFile checks if the file extension is a supported video format.
func isVideoFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	_, ok := videoContentTypes[ext]
	return ok
}

// isPlaylistFile checks if a file is an M3U8 playlist.
func isPlaylistFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".m3u8" || ext == ".m3u"
}

// parseM3U8 parses an M3U8 playlist file and returns the list of songs.
func parseM3U8(path string) ([]PlaylistSong, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var songs []PlaylistSong
	scanner := bufio.NewScanner(file)
	var currentTitle string
	var currentDuration int

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if strings.HasPrefix(line, "#EXTM3U") {
			continue // Header
		}

		if strings.HasPrefix(line, "#EXTINF:") {
			// Parse #EXTINF:duration,title
			info := strings.TrimPrefix(line, "#EXTINF:")
			parts := strings.SplitN(info, ",", 2)
			if len(parts) >= 1 {
				currentDuration, _ = strconv.Atoi(strings.TrimSpace(parts[0]))
			}
			if len(parts) >= 2 {
				currentTitle = strings.TrimSpace(parts[1])
			}
			continue
		}

		if strings.HasPrefix(line, "#") || line == "" {
			continue // Comment or empty
		}

		// This is a file path
		title := currentTitle
		if title == "" {
			title = filepath.Base(line)
		}
		songs = append(songs, PlaylistSong{
			Path:     line,
			Title:    title,
			Duration: currentDuration,
		})
		currentTitle = ""
		currentDuration = 0
	}

	return songs, scanner.Err()
}

// writeM3U8 writes a playlist to an M3U8 file.
func writeM3U8(path string, songs []PlaylistSong) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	// Write header
	fmt.Fprintln(file, "#EXTM3U")

	for _, song := range songs {
		// Write EXTINF line
		fmt.Fprintf(file, "#EXTINF:%d,%s\n", song.Duration, song.Title)
		// Write path
		fmt.Fprintln(file, song.Path)
	}

	return nil
}
