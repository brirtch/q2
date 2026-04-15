package migrations

import "jukel.org/q2/db"

func init() {
	db.Register(db.Migration{
		ID: "012_add_metadata_indexes",
		Up: func(d *db.DB) error {
			stmts := []string{
				`CREATE INDEX IF NOT EXISTS idx_audio_metadata_file_id ON audio_metadata(file_id)`,
				`CREATE INDEX IF NOT EXISTS idx_image_metadata_file_id ON image_metadata(file_id)`,
				`CREATE INDEX IF NOT EXISTS idx_album_items_album_id ON album_items(album_id)`,
			}
			for _, s := range stmts {
				if r := d.Write(s); r.Err != nil {
					return r.Err
				}
			}
			return nil
		},
		Down: func(d *db.DB) error {
			stmts := []string{
				`DROP INDEX IF EXISTS idx_audio_metadata_file_id`,
				`DROP INDEX IF EXISTS idx_image_metadata_file_id`,
				`DROP INDEX IF EXISTS idx_album_items_album_id`,
			}
			for _, s := range stmts {
				if r := d.Write(s); r.Err != nil {
					return r.Err
				}
			}
			return nil
		},
	})
}
