package hook_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/3axapp/auto-media-downloader-client/internal/config"
	"github.com/3axapp/auto-media-downloader-client/internal/hook"
	"github.com/3axapp/auto-media-downloader-client/internal/spool"
	"github.com/3axapp/auto-media-downloader-client/internal/state"
)

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	dir := t.TempDir()
	return &config.Config{
		BaseURL:  "http://nas:8080",
		APIToken: "t",
		WatchDir: filepath.Join(dir, "watch"),
		Dir:      dir,
	}
}

func envFrom(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestRunWritesReportForKnownTorrent(t *testing.T) {
	cfg := testConfig(t)
	st, _ := state.Open(cfg.StatePath())
	st.Put("Mad.Men.S07E06.1080p.rus.LostFilm.TV.mkv", state.Entry{
		JobID:       42,
		TorrentName: "Mad.Men.S07E06.1080p.rus.LostFilm.TV.mkv.torrent",
		EnqueuedAt:  time.Now(),
	})

	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	report, err := hook.Run(cfg, envFrom(map[string]string{
		"TR_TORRENT_NAME": "Mad.Men.S07E06.1080p.rus.LostFilm.TV.mkv",
		"TR_TORRENT_DIR":  `C:\torrents\done`,
	}), now)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if report.JobID != 42 {
		t.Errorf("JobID = %d, ожидалось 42", report.JobID)
	}
	if report.Status != "ok" {
		t.Errorf("Status = %q, ожидалось ok", report.Status)
	}
	wantPath := filepath.Join(`C:\torrents\done`, "Mad.Men.S07E06.1080p.rus.LostFilm.TV.mkv")
	if report.Path != wantPath {
		t.Errorf("Path = %q, ожидалось %q", report.Path, wantPath)
	}

	sp, _ := spool.Open(cfg.SpoolDir())
	entries, _ := sp.List()
	if len(entries) != 1 || entries[0].Report.JobID != 42 {
		t.Fatalf("в спуле %d записей: %+v", len(entries), entries)
	}
}

func TestRunWritesReportForUnknownTorrent(t *testing.T) {
	cfg := testConfig(t)
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)

	report, err := hook.Run(cfg, envFrom(map[string]string{
		"TR_TORRENT_NAME": "Что.То.Постороннее.mkv",
		"TR_TORRENT_DIR":  "/downloads",
	}), now)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if report.JobID != 0 {
		t.Errorf("JobID = %d, для неизвестного торрента ожидался 0", report.JobID)
	}
	if report.ContentName != "Что.То.Постороннее.mkv" {
		t.Errorf("ContentName = %q", report.ContentName)
	}

	sp, _ := spool.Open(cfg.SpoolDir())
	entries, _ := sp.List()
	if len(entries) != 1 {
		t.Fatalf("в спуле %d записей, ожидалась 1 - событие терять нельзя", len(entries))
	}
}

func TestRunFailsWithoutTorrentName(t *testing.T) {
	cfg := testConfig(t)
	_, err := hook.Run(cfg, envFrom(map[string]string{}), time.Now())
	if err == nil {
		t.Fatal("ожидалась ошибка: хук вызван вне transmission")
	}
}

func TestRunDoesNotNeedNetwork(t *testing.T) {
	// base_url заведомо нерабочий: done обязан отработать локально.
	cfg := testConfig(t)
	cfg.BaseURL = "http://127.0.0.1:1"

	if _, err := hook.Run(cfg, envFrom(map[string]string{
		"TR_TORRENT_NAME": "Что.То.mkv",
		"TR_TORRENT_DIR":  "/downloads",
	}), time.Now()); err != nil {
		t.Fatalf("Run обратился к сети или упал: %v", err)
	}
}
