package state_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/3axapp/auto-media-downloader-client/internal/state"
)

func TestContentNameStripsExtension(t *testing.T) {
	got := state.ContentName("Mad.Men.S07E06.1080p.rus.LostFilm.TV.mkv.torrent")
	want := "Mad.Men.S07E06.1080p.rus.LostFilm.TV.mkv"
	if got != want {
		t.Fatalf("ContentName = %q, ожидалось %q", got, want)
	}
}

func TestOpenMissingFileGivesEmptyStore(t *testing.T) {
	s, err := state.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, ok := s.Lookup("что угодно"); ok {
		t.Fatal("в пустом состоянии не должно быть записей")
	}
}

func TestPutThenLookup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, _ := state.Open(path)

	entry := state.Entry{
		JobID:       42,
		TorrentName: "Mad.Men.S07E06.1080p.rus.LostFilm.TV.mkv.torrent",
		EnqueuedAt:  time.Now().UTC().Truncate(time.Second),
	}
	if err := s.Put("Mad.Men.S07E06.1080p.rus.LostFilm.TV.mkv", entry); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, ok := s.Lookup("Mad.Men.S07E06.1080p.rus.LostFilm.TV.mkv")
	if !ok {
		t.Fatal("запись не найдена")
	}
	if got.JobID != 42 {
		t.Errorf("JobID = %d, ожидалось 42", got.JobID)
	}
}

func TestPutPersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, _ := state.Open(path)
	s.Put("контент", state.Entry{JobID: 7, EnqueuedAt: time.Now()})

	reopened, err := state.Open(path)
	if err != nil {
		t.Fatalf("повторный Open: %v", err)
	}
	got, ok := reopened.Lookup("контент")
	if !ok || got.JobID != 7 {
		t.Fatalf("после перечитывания запись = %+v, ok = %v", got, ok)
	}
}

func TestDeleteRemovesEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, _ := state.Open(path)
	s.Put("контент", state.Entry{JobID: 7, EnqueuedAt: time.Now()})

	if err := s.Delete("контент"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := s.Lookup("контент"); ok {
		t.Fatal("запись должна была исчезнуть")
	}

	reopened, _ := state.Open(path)
	if _, ok := reopened.Lookup("контент"); ok {
		t.Fatal("удаление не сохранилось на диск")
	}
}

func TestOlderThanSelectsStaleEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, _ := state.Open(path)
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)

	s.Put("старое", state.Entry{JobID: 1, EnqueuedAt: now.Add(-100 * time.Hour)})
	s.Put("свежее", state.Entry{JobID: 2, EnqueuedAt: now.Add(-1 * time.Hour)})

	stale := s.OlderThan(72*time.Hour, now)
	if len(stale) != 1 {
		t.Fatalf("просроченных %d, ожидалась 1: %+v", len(stale), stale)
	}
	if stale[0].ContentName != "старое" || stale[0].Entry.JobID != 1 {
		t.Errorf("выбрана не та запись: %+v", stale[0])
	}
}

func TestOpenRejectsBrokenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	os.WriteFile(path, []byte("это не json"), 0o644)

	if _, err := state.Open(path); err == nil {
		t.Fatal("ожидалась ошибка на повреждённом состоянии")
	}
}
