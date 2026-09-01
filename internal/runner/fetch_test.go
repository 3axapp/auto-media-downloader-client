package runner_test

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
	"github.com/3axapp/auto-media-downloader-client/internal/runner"
	"github.com/3axapp/auto-media-downloader-client/internal/spool"
	"github.com/3axapp/auto-media-downloader-client/internal/state"
)

// harness — фейковый демон плюс собранный на него Runner.
type harness struct {
	cfg       *config.Config
	runner    *runner.Runner
	state     *state.Store
	spool     *spool.Spool
	acked     []int
	ackStatus int
	jobsReq   []string
	complete  []api.CompleteRequest
	server    *httptest.Server

	completeStatus int
	completeBody   string
	completeCalls  int
}

const torrentBody = "d8:announce20:http://example/annce4:infod4:name40:" +
	"Mad.Men.S07E06.1080p.rus.LostFilm.TV.mkv6:lengthi1024ee"

func jobsJSON() string {
	return `[{"id":42,"seriesId":1136,"seriesName":"Безумцы","seriesNameEn":"Mad Men",
		"season":7,"episode":6,"quality":"1080",
		"torrentName":"Mad.Men.S07E06.1080p.rus.LostFilm.TV.mkv.torrent",
		"torrentUrl":"/jobs/42/torrent","leaseUntil":"2026-08-25T18:40:00Z"}]`
}

func newHarness(t *testing.T, jobs string, torrentStatus int) *harness {
	t.Helper()
	h := &harness{}

	h.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/jobs":
			h.jobsReq = append(h.jobsReq, r.URL.Query().Get("limit"))
			w.Write([]byte(jobs))
		case strings.HasSuffix(r.URL.Path, "/torrent"):
			if torrentStatus != http.StatusOK {
				w.WriteHeader(torrentStatus)
				w.Write([]byte(`{"error":"байты торрента недоступны"}`))
				return
			}
			w.Header().Set("Content-Type", "application/x-bittorrent")
			w.Write([]byte(torrentBody))
		case strings.HasSuffix(r.URL.Path, "/ack"):
			if h.ackStatus != 0 && h.ackStatus != http.StatusNoContent {
				w.WriteHeader(h.ackStatus)
				return
			}
			h.acked = append(h.acked, 42)
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/hooks/complete":
			h.completeCalls++
			if h.completeStatus != 0 && h.completeStatus != http.StatusNoContent {
				w.WriteHeader(h.completeStatus)
				w.Write([]byte(h.completeBody))
				return
			}
			var req api.CompleteRequest
			body, _ := io.ReadAll(r.Body)
			_ = jsonUnmarshal(body, &req)
			h.complete = append(h.complete, req)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(h.server.Close)

	dir := t.TempDir()
	h.cfg = &config.Config{
		BaseURL:     h.server.URL,
		APIToken:    "секрет",
		WatchDir:    filepath.Join(dir, "watch"),
		JobsPerPoll: 5,
		// Нулевой таймаут признал бы просроченной любую запись состояния:
		// OlderThan(0, now) отбирает всё, что раньше now.
		DownloadTimeout: config.Duration(72 * time.Hour),
		Dir:             dir,
	}
	if err := os.MkdirAll(h.cfg.WatchDir, 0o755); err != nil {
		t.Fatalf("создание watch-папки: %v", err)
	}

	var err error
	if h.state, err = state.Open(h.cfg.StatePath()); err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	if h.spool, err = spool.Open(h.cfg.SpoolDir()); err != nil {
		t.Fatalf("spool.Open: %v", err)
	}

	client := api.New(h.cfg.BaseURL, h.cfg.APIToken, 5*time.Second)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h.runner = runner.New(h.cfg, client, h.state, h.spool, log)
	h.runner.Now = func() time.Time { return time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC) }
	return h
}

func TestFetchJobsPlacesTorrentAndAcks(t *testing.T) {
	h := newHarness(t, jobsJSON(), http.StatusOK)

	if err := h.runner.FetchJobs(context.Background()); err != nil {
		t.Fatalf("FetchJobs: %v", err)
	}

	placed := filepath.Join(h.cfg.WatchDir, "Mad.Men.S07E06.1080p.rus.LostFilm.TV.mkv.torrent")
	got, err := os.ReadFile(placed)
	if err != nil {
		t.Fatalf("торрент не попал в watch-папку: %v", err)
	}
	if string(got) != torrentBody {
		t.Errorf("содержимое торрента искажено")
	}
	if len(h.acked) != 1 {
		t.Errorf("ack вызван %d раз, ожидался 1", len(h.acked))
	}
	entry, ok := h.state.Lookup("Mad.Men.S07E06.1080p.rus.LostFilm.TV.mkv")
	if !ok || entry.JobID != 42 {
		t.Errorf("состояние = %+v, ok = %v", entry, ok)
	}
}

func TestFetchJobsRequestsConfiguredLimit(t *testing.T) {
	h := newHarness(t, `[]`, http.StatusOK)
	h.cfg.JobsPerPoll = 3

	if err := h.runner.FetchJobs(context.Background()); err != nil {
		t.Fatalf("FetchJobs: %v", err)
	}
	if len(h.jobsReq) != 1 || h.jobsReq[0] != "3" {
		t.Fatalf("limit = %v, ожидался 3 — GET /jobs сжигает attempts", h.jobsReq)
	}
}

func TestFetchJobsLeavesNoPartialFileInWatchDir(t *testing.T) {
	h := newHarness(t, jobsJSON(), http.StatusOK)

	if err := h.runner.FetchJobs(context.Background()); err != nil {
		t.Fatalf("FetchJobs: %v", err)
	}

	entries, _ := os.ReadDir(h.cfg.WatchDir)
	for _, e := range entries {
		if e.IsDir() {
			continue // .amd-tmp
		}
		if !strings.HasSuffix(e.Name(), ".torrent") {
			t.Errorf("в watch-папке посторонний файл %q — transmission увидел бы обрывок", e.Name())
		}
	}
}

func TestFetchJobsDoesNotAckWhenPlacementFails(t *testing.T) {
	h := newHarness(t, jobsJSON(), http.StatusOK)
	// Занимаем путь .amd-tmp обычным файлом: MkdirAll упадёт всегда,
	// в том числе под root.
	if err := os.WriteFile(h.cfg.TmpDir(), []byte("занято"), 0o644); err != nil {
		t.Fatalf("подготовка: %v", err)
	}

	_ = h.runner.FetchJobs(context.Background())

	if len(h.acked) != 0 {
		t.Fatal("ack ушёл при неудачной записи — байты торрента на сервере обнулены, файл потерян")
	}
	if _, ok := h.state.Lookup("Mad.Men.S07E06.1080p.rus.LostFilm.TV.mkv"); ok {
		t.Error("осталась запись состояния о закачке, которой нет")
	}
}

func TestFetchJobsDoesNotAckWhenTorrentGone(t *testing.T) {
	h := newHarness(t, jobsJSON(), http.StatusNotFound)

	_ = h.runner.FetchJobs(context.Background())

	if len(h.acked) != 0 {
		t.Fatal("ack ушёл, хотя байты торрента не получены")
	}
}

func TestFetchJobsKeepsStateWhenAckFails(t *testing.T) {
	h := newHarness(t, jobsJSON(), http.StatusOK)
	h.ackStatus = http.StatusInternalServerError

	_ = h.runner.FetchJobs(context.Background())

	// Файл уже у transmission, значит хук однажды сработает — запись
	// состояния обязана остаться, иначе отчёт будет некуда привязать.
	if _, err := os.Stat(filepath.Join(h.cfg.WatchDir,
		"Mad.Men.S07E06.1080p.rus.LostFilm.TV.mkv.torrent")); err != nil {
		t.Fatalf("торрент не в watch-папке: %v", err)
	}
	if _, ok := h.state.Lookup("Mad.Men.S07E06.1080p.rus.LostFilm.TV.mkv"); !ok {
		t.Fatal("запись состояния пропала из-за неудачного ack — отчёт будет потерян")
	}
}

func TestFetchJobsRejectsPathTraversal(t *testing.T) {
	jobs := `[{"id":43,"seriesId":1,"seriesName":"","seriesNameEn":"","season":1,"episode":1,
		"quality":"SD","torrentName":"../../evil.torrent","torrentUrl":"/jobs/43/torrent",
		"leaseUntil":null}]`
	h := newHarness(t, jobs, http.StatusOK)

	_ = h.runner.FetchJobs(context.Background())

	if _, err := os.Stat(filepath.Join(filepath.Dir(h.cfg.WatchDir), "..", "evil.torrent")); err == nil {
		t.Fatal("файл записан за пределы watch-папки")
	}
	if len(h.acked) != 0 {
		t.Error("ack ушёл по заданию с недопустимым именем")
	}
}

func jsonUnmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

// stateEntry — короткая запись состояния для тестов.
func stateEntry(jobID int) state.Entry {
	return state.Entry{
		JobID:       jobID,
		TorrentName: "Mad.Men.S07E06.1080p.rus.LostFilm.TV.mkv.torrent",
		EnqueuedAt:  time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC),
	}
}

// newHarnessComplete поднимает демона, у которого /hooks/complete всегда
// отвечает заданным кодом.
func newHarnessComplete(t *testing.T, status int, body string) *harness {
	t.Helper()
	h := newHarness(t, `[]`, http.StatusOK)
	h.completeStatus, h.completeBody = status, body
	return h
}
