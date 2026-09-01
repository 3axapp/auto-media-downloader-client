package spool_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/3axapp/auto-media-downloader-client/internal/spool"
)

func TestOpenCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "spool")
	if _, err := spool.Open(dir); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("каталог не создан: %v", err)
	}
}

func TestWriteThenList(t *testing.T) {
	s, _ := spool.Open(filepath.Join(t.TempDir(), "spool"))

	report := spool.Report{
		JobID:       42,
		Status:      "ok",
		Path:        `C:\torrents\Mad.Men.S07E06.mkv`,
		ContentName: "Mad.Men.S07E06.1080p.rus.LostFilm.TV.mkv",
		CreatedAt:   time.Now().UTC().Truncate(time.Second),
	}
	if _, err := s.Write(report); err != nil {
		t.Fatalf("Write: %v", err)
	}

	entries, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("записей %d, ожидалась 1", len(entries))
	}
	if entries[0].Report.JobID != 42 || entries[0].Report.Status != "ok" {
		t.Errorf("отчёт прочитан неверно: %+v", entries[0].Report)
	}
}

func TestListIsSortedByTime(t *testing.T) {
	s, _ := spool.Open(filepath.Join(t.TempDir(), "spool"))
	base := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)

	s.Write(spool.Report{JobID: 2, Status: "ok", CreatedAt: base.Add(2 * time.Second)})
	s.Write(spool.Report{JobID: 1, Status: "ok", CreatedAt: base})

	entries, _ := s.List()
	if len(entries) != 2 {
		t.Fatalf("записей %d, ожидалось 2", len(entries))
	}
	if entries[0].Report.JobID != 1 {
		t.Errorf("первым идёт задание %d, ожидалось 1", entries[0].Report.JobID)
	}
}

func TestWriteTwiceDoesNotCollide(t *testing.T) {
	s, _ := spool.Open(filepath.Join(t.TempDir(), "spool"))
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)

	s.Write(spool.Report{JobID: 42, Status: "ok", CreatedAt: now})
	s.Write(spool.Report{JobID: 42, Status: "ok", CreatedAt: now})

	entries, _ := s.List()
	if len(entries) != 2 {
		t.Fatalf("записей %d, ожидалось 2 - отчёты с одинаковым временем затирают друг друга", len(entries))
	}
}

func TestRemove(t *testing.T) {
	s, _ := spool.Open(filepath.Join(t.TempDir(), "spool"))
	path, _ := s.Write(spool.Report{JobID: 1, Status: "ok", CreatedAt: time.Now()})

	if err := s.Remove(path); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	entries, _ := s.List()
	if len(entries) != 0 {
		t.Fatalf("после удаления осталось %d записей", len(entries))
	}
}

func TestListIgnoresForeignFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "spool")
	s, _ := spool.Open(dir)
	os.WriteFile(filepath.Join(dir, "заметка.txt"), []byte("не отчёт"), 0o644)
	s.Write(spool.Report{JobID: 1, Status: "ok", CreatedAt: time.Now()})

	entries, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("записей %d, ожидалась 1 - посторонние файлы должны игнорироваться", len(entries))
	}
}
