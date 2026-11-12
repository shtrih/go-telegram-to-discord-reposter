package database

import (
	"time"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"reposter/config"
)

type Database struct {
	Conn *gorm.DB
	conf *config.Config
}

func (db *Database) AutoMigrate() error {
	if err := db.Conn.AutoMigrate(&Post{}).Error; err != nil {
		return err
	}

	return nil
}

// Cleanup hard-deletes posts older than window (the edit window) so the
// database does not grow forever. window <= 0 disables cleanup.
// Rows deleted by soft delete (gorm.DeletedAt) are purged too, then the
// file is compacted with VACUUM so deleted space is actually reclaimed.
func (db *Database) Cleanup(window time.Duration) (int64, error) {
	if window <= 0 {
		return 0, nil
	}

	res := db.Conn.Unscoped().Where("created_at < ?", time.Now().Add(-window)).Delete(&Post{})
	if res.Error != nil {
		return 0, res.Error
	}
	if res.RowsAffected > 0 {
		if _, err := db.Conn.DB().Exec("VACUUM"); err != nil {
			return res.RowsAffected, err
		}
	}

	return res.RowsAffected, nil
}

func NewDatabase(conf *config.Config) (*Database, error) {
	conn, err := gorm.Open("sqlite3", conf.Database)
	if err != nil {
		return nil, err
	}

	// SQLite allows a single writer; serialize all access (cleanup runs in
	// its own goroutine) instead of racing into "database is locked".
	conn.DB().SetMaxOpenConns(1)

	return &Database{
		Conn: conn,
		conf: conf,
	}, nil
}
