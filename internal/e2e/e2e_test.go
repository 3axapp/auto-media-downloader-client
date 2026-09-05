package e2e_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/3axapp/auto-media-downloader-client/internal/api"
	"github.com/3axapp/auto-media-downloader-client/internal/config"
	"github.com/3axapp/auto-media-downloader-client/internal/hook"
	"github.com/3axapp/auto-media-downloader-client/internal/runner"
	"github.com/3axapp/auto-media-downloader-client/internal/spool"
	"github.com/3axapp/auto-media-downloader-client/internal/state"
)

const torrentName = "Mad.Men.S07E06.1080p.rus.LostFilm.TV.mkv.torrent"
const contentName = "Mad.Men.S07E06.1080p.rus.LostFilm.TV.mkv"

// daemon — заглушка демона: выдаёт одно задание, отдаёт байты торрента,
// принимает ack и отчёт.
type daemon struct {
	torrent   []byte
	handedOut bool
	acked     bool
	completed []api.CompleteRequest
}

func (d *daemon) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/jobs":
			if d.handedOut {
				w.Write([]byte(`[]`))
				return
			}
			d.handedOut = true
			w.Write([]byte(`[{"id":42,"seriesId":1136,"seriesName":"Безумцы","seriesNameEn":"Mad Men",
				"season":7,"episode":6,"quality":"1080","torrentName":"` + torrentName + `",
				"torrentUrl":"/jobs/42/torrent","leaseUntil":"2026-08-25T18:40:00Z"}]`))
		case r.URL.Path == "/jobs/42/torrent":
			if d.acked {
				// После ack байты обнуляются — как у настоящего демона.
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"error":"байты торрента недоступны"}`))
				return
			}
			w.Header().Set("Content-Type", "application/x-bittorrent")
			w.Header().Set("Content-Disposition", `attachment; filename="`+torrentName+`"`)
			w.Write(d.torrent)
		case r.URL.Path == "/jobs/42/ack":
			d.acked = true
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/hooks/complete":
			var req api.CompleteRequest
			body, _ := io.ReadAll(r.Body)
			json.Unmarshal(body, &req)
			d.completed = append(d.completed, req)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func TestFullCycle(t *testing.T) {
	torrent, err := os.ReadFile(filepath.Join("testdata", "sample.torrent"))
	if err != nil {
		t.Fatalf("фикстура торрента: %v", err)
	}

	d := &daemon{torrent: torrent}
	srv := httptest.NewServer(d.handler())
	defer srv.Close()

	dir := t.TempDir()
	cfg := &config.Config{
		BaseURL:         srv.URL,
		APIToken:        "секрет",
		WatchDir:        filepath.Join(dir, "watch"),
		JobsPerPoll:     5,
		DownloadTimeout: config.Duration(72 * time.Hour),
		HTTPTimeout:     config.Duration(5 * time.Second),
		Dir:             dir,
	}

	st, err := state.Open(cfg.StatePath())
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	sp, err := spool.Open(cfg.SpoolDir())
	if err != nil {
		t.Fatalf("spool.Open: %v", err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	r := runner.New(cfg, api.New(cfg.BaseURL, cfg.APIToken, 5*time.Second), st, sp, log)
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	r.Now = func() time.Time { return now }

	// Цикл 1: задание забрано, торрент в watch-папке, ack отправлен.
	r.Tick(context.Background())

	placed := filepath.Join(cfg.WatchDir, torrentName)
	body, err := os.ReadFile(placed)
	if err != nil {
		t.Fatalf("торрент не попал в watch-папку: %v", err)
	}
	if len(body) != len(torrent) {
		t.Errorf("размер торрента %d, ожидался %d", len(body), len(torrent))
	}
	if !d.acked {
		t.Fatal("ack не отправлен")
	}
	if _, ok := st.Lookup(contentName); !ok {
		t.Fatalf("нет записи состояния по ключу %q", contentName)
	}

	// Transmission скачал файл и вызвал хук.
	downloadDir := filepath.Join(dir, "downloads")
	env := func(k string) string {
		switch k {
		case "TR_TORRENT_NAME":
			return contentName
		case "TR_TORRENT_DIR":
			return downloadDir
		}
		return ""
	}
	if _, err := hook.Run(cfg, env, now.Add(time.Hour)); err != nil {
		t.Fatalf("hook.Run: %v", err)
	}

	// Цикл 2: отчёт доставлен, состояние очищено, спул пуст.
	r.Tick(context.Background())

	if len(d.completed) != 1 {
		t.Fatalf("отчётов доставлено %d, ожидался 1: %+v", len(d.completed), d.completed)
	}
	report := d.completed[0]
	if report.JobID != 42 || report.Status != "ok" {
		t.Errorf("отчёт = %+v", report)
	}
	if !strings.Contains(report.Path, contentName) {
		t.Errorf("в отчёте нет пути к файлу: %q", report.Path)
	}
	if _, ok := st.Lookup(contentName); ok {
		t.Error("запись состояния осталась после доставки отчёта")
	}
	entries, _ := sp.List()
	if len(entries) != 0 {
		t.Errorf("в спуле осталось %d записей", len(entries))
	}
}

func TestHookSurvivesDeadDaemon(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		BaseURL:  "http://127.0.0.1:1", // заведомо мёртвый
		APIToken: "секрет",
		WatchDir: filepath.Join(dir, "watch"),
		Dir:      dir,
	}
	st, _ := state.Open(cfg.StatePath())
	st.Put(contentName, state.Entry{JobID: 42, TorrentName: torrentName, EnqueuedAt: time.Now()})

	start := time.Now()
	report, err := hook.Run(cfg, func(k string) string {
		switch k {
		case "TR_TORRENT_NAME":
			return contentName
		case "TR_TORRENT_DIR":
			return filepath.Join(dir, "downloads")
		}
		return ""
	}, time.Now())
	if err != nil {
		t.Fatalf("хук упал при недоступном демоне: %v", err)
	}
	if report.JobID != 42 {
		t.Errorf("JobID = %d", report.JobID)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("хук работал %v — transmission нельзя задерживать", elapsed)
	}
}
