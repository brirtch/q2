package media

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cespare/xxhash/v2"
	"jukel.org/q2/ffmpeg"
)

const (
	SmallThumbnailSize    = 500
	LargeThumbnailSize    = 1800
	ThumbnailQuality      = 3 // FFmpeg qscale:v (2-5 is high quality, ~85%)
	ThumbnailDir          = "thumbnails"
)

// ThumbnailResult contains the result of thumbnail generation.
type ThumbnailResult struct {
	SmallPath string // Relative path to small thumbnail
	LargePath string // Relative path to large thumbnail
	Error     error
}

// getHashSubfolder returns the first 2 characters of the hash for subfolder sharding
func getHashSubfolder(hash string) string {
	if len(hash) >= 2 {
		return hash[:2]
	}
	return "00"
}

// GenerateThumbnail creates a thumbnail for the given image file using FFmpeg.
// Returns the relative path to the thumbnail within the q2Dir.
// Skips generation if thumbnail exists and is newer than the source file.
func GenerateThumbnail(ctx context.Context, imagePath, q2Dir string, size int, ffmpegMgr *ffmpeg.Manager) (string, error) {
	if ffmpegMgr == nil {
		return "", fmt.Errorf("ffmpeg manager not available")
	}

	// Get source file info for mtime comparison
	srcInfo, err := os.Stat(imagePath)
	if err != nil {
		return "", fmt.Errorf("cannot stat source file: %w", err)
	}

	// Generate hash of original path for filename
	hash := fmt.Sprintf("%016x", xxhash.Sum64String(strings.ToLower(imagePath)))
	subfolder := getHashSubfolder(hash)

	// Thumbnail filename includes size for uniqueness
	thumbFilename := fmt.Sprintf("%s_%d.jpg", hash, size)
	thumbRelPath := filepath.Join(ThumbnailDir, subfolder, thumbFilename)
	thumbFullPath := filepath.Join(q2Dir, thumbRelPath)

	// Check if thumbnail already exists and is newer than source
	if thumbInfo, err := os.Stat(thumbFullPath); err == nil {
		if thumbInfo.ModTime().After(srcInfo.ModTime()) {
			// Thumbnail is up to date
			return thumbRelPath, nil
		}
	}

	// Create thumbnail directory if it doesn't exist
	thumbDir := filepath.Join(q2Dir, ThumbnailDir, subfolder)
	if err := os.MkdirAll(thumbDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create thumbnail directory: %w", err)
	}

	// Generate thumbnail using FFmpeg
	if err := ffmpegMgr.GenerateThumbnail(ctx, imagePath, thumbFullPath, size, ThumbnailQuality); err != nil {
		return "", fmt.Errorf("failed to generate thumbnail: %w", err)
	}

	return thumbRelPath, nil
}

// GenerateSmallThumbnail creates a small (500px) thumbnail.
func GenerateSmallThumbnail(ctx context.Context, imagePath, q2Dir string, ffmpegMgr *ffmpeg.Manager) (string, error) {
	return GenerateThumbnail(ctx, imagePath, q2Dir, SmallThumbnailSize, ffmpegMgr)
}

// GenerateLargeThumbnail creates a large (1800px) thumbnail.
func GenerateLargeThumbnail(ctx context.Context, imagePath, q2Dir string, ffmpegMgr *ffmpeg.Manager) (string, error) {
	return GenerateThumbnail(ctx, imagePath, q2Dir, LargeThumbnailSize, ffmpegMgr)
}

// GenerateBothThumbnails creates both small and large thumbnails for an image.
// Returns relative paths to both thumbnails.
func GenerateBothThumbnails(ctx context.Context, imagePath, q2Dir string, ffmpegMgr *ffmpeg.Manager) (smallPath, largePath string, err error) {
	smallPath, err = GenerateSmallThumbnail(ctx, imagePath, q2Dir, ffmpegMgr)
	if err != nil {
		return "", "", fmt.Errorf("small thumbnail: %w", err)
	}

	largePath, err = GenerateLargeThumbnail(ctx, imagePath, q2Dir, ffmpegMgr)
	if err != nil {
		return "", "", fmt.Errorf("large thumbnail: %w", err)
	}

	return smallPath, largePath, nil
}

// IsSupportedImageFormat checks if the file extension is a supported image format.
// FFmpeg supports many formats including HEIC, RAW, etc.
func IsSupportedImageFormat(ext string) bool {
	ext = strings.ToLower(ext)
	supported := map[string]bool{
		".jpg":  true,
		".jpeg": true,
		".png":  true,
		".gif":  true,
		".webp": true,
		".bmp":  true,
		".heic": true,
		".heif": true,
		".tiff": true,
		".tif":  true,
		".raw":  true,
		".cr2":  true,
		".nef":  true,
		".arw":  true,
	}
	return supported[ext]
}

// DeleteThumbnail removes a thumbnail file if it exists.
func DeleteThumbnail(thumbPath, q2Dir string) error {
	if thumbPath == "" {
		return nil
	}
	fullPath := filepath.Join(q2Dir, thumbPath)
	if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// GetThumbnailPath returns the expected thumbnail path for an image without generating it.
// Useful for checking if a thumbnail exists or for serving.
func GetThumbnailPath(imagePath string, size int) string {
	hash := fmt.Sprintf("%016x", xxhash.Sum64String(strings.ToLower(imagePath)))
	subfolder := getHashSubfolder(hash)
	thumbFilename := fmt.Sprintf("%s_%d.jpg", hash, size)
	return filepath.Join(ThumbnailDir, subfolder, thumbFilename)
}
