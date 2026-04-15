package migrations

import "jukel.org/q2/db"

func init() {
	db.Register(db.Migration{
		ID: "010_create_play_history",
		Up: func(d *db.DB) error {
			result := d.Write(`
				CREATE TABLE play_history (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					file_id INTEGER NOT NULL,
					played_at DATETIME DEFAULT CURRENT_TIMESTAMP,
					FOREIGN KEY (file_id) REFERENCES files(id) ON DELETE CASCADE
				)
			`)
			if result.Err != nil {
				return result.Err
			}
			result = d.Write(`CREATE INDEX idx_play_history_file_id ON play_history(file_id)`)
			if result.Err != nil {
				return result.Err
			}
			result = d.Write(`CREATE INDEX idx_play_history_played_at ON play_history(played_at)`)
			return result.Err
		},
		Down: func(d *db.DB) error {
			return d.Write("DROP TABLE play_history").Err
		},
	})
}
