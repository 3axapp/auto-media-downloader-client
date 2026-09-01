package runner_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/3axapp/auto-media-downloader-client/internal/config"
	"github.com/3axapp/auto-media-downloader-client/internal/state"
)

func TestExpireStaleReportsError(t *testing.T) {
	h := newHarness(t, `[]`, http.StatusOK)
	h.cfg.DownloadTimeout = config.Duration(72 * time.Hour)
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	h.runner.Now = func() time.Time { return now }

	h.state.Put("Зависший.mkv", state.Entry{
		JobID:       77,
		TorrentName: "Зависший.mkv.torrent",
		EnqueuedAt:  now.Add(-100 * time.Hour),
	})

	if err := h.runner.ExpireStale(context.Background()); err != nil {
		t.Fatalf("ExpireStale: %v", err)
	}

	entries, _ := h.spool.List()
	if len(entries) != 1 {
		t.Fatalf("отчётов в спуле %d, ожидался 1", len(entries))
	}
	report := entries[0].Report
	if report.JobID != 77 || report.Status != "error" {
		t.Errorf("отчёт = %+v, ожидался error по заданию 77", report)
	}
	if !strings.Contains(report.Error, "72") {
		t.Errorf("в тексте ошибки нет срока: %q", report.Error)
	}
	if _, ok := h.state.Lookup("Зависший.mkv"); ok {
		t.Error("просроченная запись осталась в состоянии")
	}
}

func TestExpireStaleKeepsFreshEntries(t *testing.T) {
	h := newHarness(t, `[]`, http.StatusOK)
	h.cfg.DownloadTimeout = config.Duration(72 * time.Hour)
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	h.runner.Now = func() time.Time { return now }

	h.state.Put("Свежий.mkv", state.Entry{
		JobID: 78, TorrentName: "Свежий.mkv.torrent", EnqueuedAt: now.Add(-time.Hour),
	})

	if err := h.runner.ExpireStale(context.Background()); err != nil {
		t.Fatalf("ExpireStale: %v", err)
	}

	entries, _ := h.spool.List()
	if len(entries) != 0 {
		t.Fatalf("свежая закачка попала в отчёты: %+v", entries)
	}
	if _, ok := h.state.Lookup("Свежий.mkv"); !ok {
		t.Error("свежая запись пропала из состояния")
	}
}

func TestTickDoesEverythingInOrder(t *testing.T) {
	h := newHarness(t, jobsJSON(), http.StatusOK)
	h.cfg.DownloadTimeout = config.Duration(72 * time.Hour)
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	h.runner.Now = func() time.Time { return now }

	h.state.Put("Зависший.mkv", state.Entry{
		JobID: 77, TorrentName: "Зависший.mkv.torrent", EnqueuedAt: now.Add(-100 * time.Hour),
	})

	h.runner.Tick(context.Background())

	// Отчёт по зависшей закачке ушёл в тот же цикл.
	var sawError bool
	for _, c := range h.complete {
		if c.JobID == 77 && c.Status == "error" {
			sawError = true
		}
	}
	if !sawError {
		t.Errorf("отчёт по зависшей закачке не доставлен: %+v", h.complete)
	}
	// Новое задание при этом выгружено.
	if len(h.acked) != 1 {
		t.Errorf("новое задание не подтверждено, ack вызван %d раз", len(h.acked))
	}
}

func TestRunStopsOnContextCancel(t *testing.T) {
	h := newHarness(t, `[]`, http.StatusOK)
	h.cfg.PollInterval = config.Duration(10 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- h.runner.Run(ctx) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run вернул ошибку: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run не завершился по отмене контекста")
	}
	if len(h.jobsReq) == 0 {
		t.Error("за время работы не было ни одного опроса заданий")
	}
}
