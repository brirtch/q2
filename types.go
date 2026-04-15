package main

// MetadataRefreshRequest is the request body for metadata refresh.
type MetadataRefreshRequest struct {
	Path string `json:"path"`
}

// MetadataRefreshResponse is the response for metadata refresh start.
type MetadataRefreshResponse struct {
	Success       bool   `json:"success"`
	Message       string `json:"message"`
	QueuePosition int    `json:"queue_position,omitempty"` // Position in queue (0 = processing now)
}

// MetadataStatusResponse is the response for metadata refresh status.
type MetadataStatusResponse struct {
	Scanning    bool     `json:"scanning"`
	Path        string   `json:"path,omitempty"`
	CurrentFile string   `json:"current_file,omitempty"`
	FilesTotal  int      `json:"files_total"`
	FilesDone   int      `json:"files_done"`
	Queue       []string `json:"queue,omitempty"`  // Paths waiting in queue
	QueueLength int      `json:"queue_length"`      // Number of items in queue
}

// RootFolder represents a monitored folder.
type RootFolder struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

// RootsResponse is the response for /api/roots.
type RootsResponse struct {
	Roots []RootFolder `json:"roots"`
}

// FileEntry represents a file or directory in a listing.
type FileEntry struct {
	Name     string `json:"name"`
	Type     string `json:"type"` // "file" or "dir"
	Size     int64  `json:"size"`
	Modified string `json:"modified"` // ISO 8601 format
	// Optional metadata fields (populated when ?metadata=true)
	MediaType      string `json:"mediaType,omitempty"`      // "image", "audio", "video", or empty
	ThumbnailSmall string `json:"thumbnailSmall,omitempty"` // URL to small thumbnail
	ThumbnailLarge string `json:"thumbnailLarge,omitempty"` // URL to large thumbnail
	// Audio-specific metadata
	Title    string `json:"title,omitempty"`
	Artist   string `json:"artist,omitempty"`
	Album    string `json:"album,omitempty"`
	Duration int    `json:"duration,omitempty"` // Duration in seconds
}

// BrowseResponse is the response for /api/browse.
type BrowseResponse struct {
	Path    string      `json:"path"`
	Parent  *string     `json:"parent"` // nil if this is a root folder
	Entries []FileEntry `json:"entries"`
}

// ErrorResponse is returned for API errors.
type ErrorResponse struct {
	Error string `json:"error"`
}

// CastPlayRequest is the request body for /api/cast/play.
type CastPlayRequest struct {
	Path        string `json:"path"`
	ContentType string `json:"content_type"`
	Title       string `json:"title"`
}

// CastConnectRequest is the request body for /api/cast/connect.
type CastConnectRequest struct {
	UUID string `json:"uuid"`
}

// CastSeekRequest is the request body for /api/cast/seek.
type CastSeekRequest struct {
	Position float64 `json:"position"`
}

// CastVolumeRequest is the request body for /api/cast/volume.
type CastVolumeRequest struct {
	Level float64 `json:"level"`
	Muted *bool   `json:"muted,omitempty"`
}

// PlaylistSong represents a song in a playlist.
type PlaylistSong struct {
	Path     string `json:"path"`
	Title    string `json:"title"`
	Artist   string `json:"artist"`
	Album    string `json:"album"`
	Duration int    `json:"duration"` // seconds
}

// Playlist represents a playlist with metadata.
type Playlist struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Count int    `json:"count"`
}

// PlaylistWithContains adds a contains flag for checking if a song is in the playlist.
type PlaylistWithContains struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Contains bool   `json:"contains"`
}

// PlaylistResponse is the response for reading a playlist.
type PlaylistResponse struct {
	Name  string         `json:"name"`
	Path  string         `json:"path"`
	Songs []PlaylistSong `json:"songs"`
}

// PlaylistsResponse is the response for listing playlists.
type PlaylistsResponse struct {
	Playlists []Playlist `json:"playlists"`
}

// PlaylistCheckResponse is the response for checking which playlists contain a song.
type PlaylistCheckResponse struct {
	Playlists []PlaylistWithContains `json:"playlists"`
}

// PlaylistCreateRequest is the request body for creating a playlist.
type PlaylistCreateRequest struct {
	Name string `json:"name"`
}

// PlaylistAddRequest is the request body for adding a song to a playlist.
type PlaylistAddRequest struct {
	Playlist string `json:"playlist"`
	Song     string `json:"song"`
	Title    string `json:"title"`
	Duration int    `json:"duration"`
}

// PlaylistRemoveRequest is the request body for removing a song from a playlist.
type PlaylistRemoveRequest struct {
	Playlist string `json:"playlist"`
	Index    int    `json:"index"`
}

// PlaylistReorderRequest is the request body for reordering songs in a playlist.
type PlaylistReorderRequest struct {
	Playlist  string `json:"playlist"`
	FromIndex int    `json:"from_index"`
	ToIndex   int    `json:"to_index"`
}

// Album represents a photo album stored in the database.
type Album struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	CoverPath string `json:"cover_path,omitempty"`
	ItemCount int    `json:"item_count"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// AlbumItem represents an image in an album.
type AlbumItem struct {
	ID             int64  `json:"id"`
	FileID         int64  `json:"file_id"`
	Position       int    `json:"position"`
	Path           string `json:"path"`
	Filename       string `json:"filename"`
	ThumbnailSmall string `json:"thumbnail_small,omitempty"`
	ThumbnailLarge string `json:"thumbnail_large,omitempty"`
}

// AlbumWithContains adds a contains flag for checking if an image is in the album.
type AlbumWithContains struct {
	Album
	Contains bool `json:"contains"`
}

// AlbumsResponse is the response for listing albums.
type AlbumsResponse struct {
	Albums []Album `json:"albums"`
}

// AlbumResponse is the response for reading an album.
type AlbumResponse struct {
	Album Album       `json:"album"`
	Items []AlbumItem `json:"items"`
}

// AlbumCheckResponse is the response for checking which albums contain an image.
type AlbumCheckResponse struct {
	Albums []AlbumWithContains `json:"albums"`
}

// AlbumCreateRequest is the request body for creating an album.
type AlbumCreateRequest struct {
	Name string `json:"name"`
}

// AlbumAddRequest is the request body for adding an image to an album.
type AlbumAddRequest struct {
	AlbumID int64  `json:"album_id"`
	Path    string `json:"path"`
}

// AlbumRemoveRequest is the request body for removing an image from an album.
type AlbumRemoveRequest struct {
	AlbumID int64 `json:"album_id"`
	ItemID  int64 `json:"item_id"`
}

// AlbumReorderRequest is the request body for reordering images in an album.
type AlbumReorderRequest struct {
	AlbumID   int64 `json:"album_id"`
	FromIndex int   `json:"from_index"`
	ToIndex   int   `json:"to_index"`
}

// LyricsResponse is the JSON response for /api/lyrics.
type LyricsResponse struct {
	SyncedLyrics string `json:"synced_lyrics"`
	PlainLyrics  string `json:"plain_lyrics"`
}

// ColumnInfo holds information about a table column.
type ColumnInfo struct {
	CID     int
	Name    string
	Type    string
	NotNull bool
	Default *string
	PK      int
}

// TableInfo holds schema information for a table.
type TableInfo struct {
	Name    string
	SQL     string
	Columns []ColumnInfo
}

// IndexInfo holds information about an index.
type IndexInfo struct {
	Name   string
	Table  string
	SQL    string
	Unique bool
}

// InboxFileStatus tracks the processing state of an uploaded file.
type InboxFileStatus struct {
	Name   string `json:"name"`
	Status string `json:"status"` // "pending", "processing", "done", "error"
	Dest   string `json:"dest,omitempty"`
	Error  string `json:"error,omitempty"`
}
