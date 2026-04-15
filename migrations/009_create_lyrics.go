package migrations

import "jukel.org/q2/db"

func init() {
	db.Register(db.Migration{
		ID: "009_create_lyrics",
		Up: func(d *db.DB) error {
			result := d.Write(`
				CREATE TABLE lyrics (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					file_id INTEGER NOT NULL UNIQUE,
					synced_lyrics TEXT,
					plain_lyrics TEXT,
					fetched_at DATETIME DEFAULT CURRENT_TIMESTAMP,
					FOREIGN KEY (file_id) REFERENCES files(id) ON DELETE CASCADE
				)
			`)
			if result.Err != nil {
				return result.Err
			}
			result = d.Write(`CREATE INDEX idx_lyrics_file_id ON lyrics(file_id)`)
			return result.Err
		},
		Down: func(d *db.DB) error {
			return d.Write("DROP TABLE lyrics").Err
		},
	})
}
