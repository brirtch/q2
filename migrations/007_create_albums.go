package migrations

import (
	"jukel.org/q2/db"
)

func init() {
	db.Register(db.Migration{
		ID: "007_create_albums",
		Up: func(d *db.DB) error {
			// Create albums table
			result := d.Write(`
				CREATE TABLE albums (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					name TEXT NOT NULL,
					cover_path TEXT,
					created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
					updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
				)
			`)
			if result.Err != nil {
				return result.Err
			}

			// Create album_items table for many-to-many relationship
			result = d.Write(`
				CREATE TABLE album_items (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					album_id INTEGER NOT NULL,
					file_id INTEGER NOT NULL,
					position INTEGER NOT NULL DEFAULT 0,
					added_at DATETIME DEFAULT CURRENT_TIMESTAMP,
					FOREIGN KEY (album_id) REFERENCES albums(id) ON DELETE CASCADE,
					FOREIGN KEY (file_id) REFERENCES files(id) ON DELETE CASCADE,
					UNIQUE(album_id, file_id)
				)
			`)
			if result.Err != nil {
				return result.Err
			}

			result = d.Write(`CREATE INDEX idx_album_items_album_id ON album_items(album_id)`)
			if result.Err != nil {
				return result.Err
			}

			result = d.Write(`CREATE INDEX idx_album_items_file_id ON album_items(file_id)`)
			return result.Err
		},
		Down: func(d *db.DB) error {
			result := d.Write("DROP TABLE album_items")
			if result.Err != nil {
				return result.Err
			}
			result = d.Write("DROP TABLE albums")
			return result.Err
		},
	})
}
