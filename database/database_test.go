package database

import (
	"testing"
	"time"

	"github.com/jinzhu/gorm"

	"reposter/config"
)

func TestCleanup(t *testing.T) {
	db, err := NewDatabase(&config.Config{Database: "file::memory:"})
	if err != nil {
		t.Fatalf("cannot open database: %s", err)
	}
	defer db.Conn.Close()

	if err := db.AutoMigrate(); err != nil {
		t.Fatalf("cannot auto migrate: %s", err)
	}

	old := &Post{Telegram: "1,1", Discord: "1"}
	fresh := &Post{Telegram: "1,2", Discord: "2"}
	for _, p := range []*Post{old, fresh} {
		pm := PostManager{DB: db.Conn, Data: p}
		if err := pm.Create(); err != nil {
			t.Fatalf("cannot create post %s: %s", p.Telegram, err)
		}
	}
	if err := db.Conn.Model(old).Update("created_at", time.Now().Add(-48*time.Hour)).Error; err != nil {
		t.Fatalf("cannot backdate post: %s", err)
	}

	find := func(telegram string) error {
		pm := PostManager{DB: db.Conn, Data: &Post{Telegram: telegram}}
		return pm.FindByTelegramPost()
	}

	n, err := db.Cleanup(24 * time.Hour)
	if err != nil {
		t.Fatalf("cleanup failed: %s", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 record removed, got %d", n)
	}
	if err := find("1,1"); err != gorm.ErrRecordNotFound {
		t.Fatalf("expected old post removed, got: %v", err)
	}
	if err := find("1,2"); err != nil {
		t.Fatalf("expected fresh post kept, got: %s", err)
	}

	// Window <= 0 disables cleanup
	if n, err = db.Cleanup(0); err != nil || n != 0 {
		t.Fatalf("expected disabled cleanup to do nothing, got (%d, %v)", n, err)
	}
	if err := find("1,2"); err != nil {
		t.Fatalf("expected fresh post kept, got: %s", err)
	}
}
