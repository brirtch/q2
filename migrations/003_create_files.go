package migrations

import (
	"jukel.org/q2/db"
)

func init() {
	db.Register(db.Migration{
		ID: "003_create_files",
		Up: func(d *db.DB) error {
			result := d.Write(`
				CREATE TABLE files (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					folder_id INTEGER NOT NULL,
					path TEXT NOT NULL UNIQUE,
					filename TEXT NOT NULL,
					extension TEXT,
					mediatype TEXT,
					size INTEGER NOT NULL,
					created_at DATETIME,
					modified_at DATETIME,
					indexed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
					thumbnail_small_path TEXT,
					xxhash TEXT,
					thumbnail_large_path TEXT,
					FOREIGN KEY (folder_id) REFERENCES folders(id) ON DELETE CASCADE
				)
			`)
			if result.Err != nil {
				return result.Err
			}

			result = d.Write(`CREATE INDEX idx_files_folder_id ON files(folder_id)`)
			if result.Err != nil {
				return result.Err
			}

			result = d.Write(`CREATE INDEX idx_files_path ON files(path)`)
			if result.Err != nil {
				return result.Err
			}

			result = d.Write(`CREATE INDEX idx_files_mediatype ON files(mediatype)`)
			return result.Err
		},
		Down: func(d *db.DB) error {
			result := d.Write("DROP TABLE files")
			return result.Err
		},
	})
}
