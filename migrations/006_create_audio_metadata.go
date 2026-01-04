package migrations

import (
	"jukel.org/q2/db"
)

func init() {
	db.Register(db.Migration{
		ID: "006_create_audio_metadata",
		Up: func(d *db.DB) error {
			result := d.Write(`
				CREATE TABLE audio_metadata (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					file_id INTEGER NOT NULL UNIQUE,
					artist TEXT,
					album TEXT,
					title TEXT,
					genre TEXT,
					track_number INTEGER,
					year INTEGER,
					duration_seconds INTEGER,
					bitrate INTEGER,
					FOREIGN KEY (file_id) REFERENCES files(id) ON DELETE CASCADE
				)
			`)
			if result.Err != nil {
				return result.Err
			}

			result = d.Write(`CREATE INDEX idx_audio_metadata_file_id ON audio_metadata(file_id)`)
			return result.Err
		},
		Down: func(d *db.DB) error {
			result := d.Write("DROP TABLE audio_metadata")
			return result.Err
		},
	})
}
