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

// SaveAudioMetadata saves audio metadata to the database, updating any existing record.
func SaveAudioMetadata(database *db.DB, fileID int64, meta *AudioMetadata) error {
	result := database.Write(`
		INSERT INTO audio_metadata (
			file_id, artist, album, title, genre,
			track_number, year, duration_seconds, bitrate
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(file_id) DO UPDATE SET
			artist          = excluded.artist,
			album           = excluded.album,
			title           = excluded.title,
			genre           = excluded.genre,
			track_number    = excluded.track_number,
			year            = excluded.year,
			duration_seconds = COALESCE(excluded.duration_seconds, duration_seconds),
			bitrate         = excluded.bitrate
	`,
		fileID, meta.Artist, meta.Album, meta.Title, meta.Genre,
		meta.TrackNumber, meta.Year, meta.DurationSeconds, meta.Bitrate,
	)
	return result.Err
}
