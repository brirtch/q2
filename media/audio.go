package media

import (
	"os"
	"strings"

	"github.com/dhowden/tag"
	"jukel.org/q2/db"
)

// AudioMetadata contains extracted ID3/audio metadata.
type AudioMetadata struct {
	Artist          *string
	Album           *string
	Title           *string
	Genre           *string
	TrackNumber     *int
	Year            *int
	DurationSeconds *int
	Bitrate         *int
}

// ExtractAudioMetadata extracts ID3/audio metadata from an audio file.
func ExtractAudioMetadata(audioPath string) (*AudioMetadata, error) {
	file, err := os.Open(audioPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	m, err := tag.ReadFrom(file)
	if err != nil {
		// No metadata or unsupported format - return empty metadata
		return &AudioMetadata{}, nil
	}

	meta := &AudioMetadata{}

	// Artist
	if artist := strings.TrimSpace(m.Artist()); artist != "" {
		meta.Artist = &artist
	}

	// Album
	if album := strings.TrimSpace(m.Album()); album != "" {
		meta.Album = &album
	}

	// Title
	if title := strings.TrimSpace(m.Title()); title != "" {
		meta.Title = &title
	}

	// Genre
	if genre := strings.TrimSpace(m.Genre()); genre != "" {
		meta.Genre = &genre
	}

	// Track number
	if track, _ := m.Track(); track > 0 {
		meta.TrackNumber = &track
	}

	// Year
	if year := m.Year(); year > 0 {
		meta.Year = &year
	}

	return meta, nil
}

// SaveAudioMetadata saves audio metadata to the database.
func SaveAudioMetadata(database *db.DB, fileID int64, meta *AudioMetadata) error {
	// Check if metadata already exists
	var existingID int64
	row := database.QueryRow("SELECT id FROM audio_metadata WHERE file_id = ?", fileID)
	if err := row.Scan(&existingID); err == nil {
		// Already exists, skip
		return nil
	}

	result := database.Write(`
		INSERT INTO audio_metadata (
			file_id, artist, album, title, genre,
			track_number, year, duration_seconds, bitrate
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		fileID, meta.Artist, meta.Album, meta.Title, meta.Genre,
		meta.TrackNumber, meta.Year, meta.DurationSeconds, meta.Bitrate,
	)

	return result.Err
}

// HasAudioMetadata checks if a file already has audio metadata.
func HasAudioMetadata(database *db.DB, fileID int64) bool {
	var id int64
	row := database.QueryRow("SELECT id FROM audio_metadata WHERE file_id = ?", fileID)
	return row.Scan(&id) == nil
}

// IsSupportedAudioFormat checks if the file extension is a supported audio format.
func IsSupportedAudioFormat(ext string) bool {
	ext = strings.ToLower(ext)
	supported := map[string]bool{
		".mp3":  true,
		".m4a":  true,
		".m4b":  true,
		".m4p":  true,
		".flac": true,
		".ogg":  true,
		".dsf":  true,
	}
	return supported[ext]
}
