package runner_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/3axapp/auto-media-downloader-client/internal/spool"
)

func TestDrainSpoolSendsAndRemoves(t *testing.T) {
	h := newHarness(t, `[]`, http.StatusOK)
	h.spool.Write(spool.Report{
		JobID: 42, Status: "ok", Path: `C:\torrents\Mad.Men.mkv`,
		CreatedAt: time.Now().UTC(),
	})

	if err := h.runner.DrainSpool(context.Background()); err != nil {
		t.Fatalf("DrainSpool: %v", err)
	}

	if len(h.complete) != 1 || h.complete[0].JobID != 42 || h.complete[0].Status != "ok" {
		t.Fatalf("отправлено %+v", h.complete)
	}
	entries, _ := h.spool.List()
	if len(entries) != 0 {
		t.Errorf("после успешной отправки в спуле осталось %d записей", len(entries))
	}
}

func TestDrainSpoolRemovesStateEntry(t *testing.T) {
	h := newHarness(t, `[]`, http.StatusOK)
	h.state.Put("Mad.Men.S07E06.1080p.rus.LostFilm.TV.mkv", stateEntry(42))
	h.spool.Write(spool.Report{
		JobID: 42, Status: "ok",
		ContentName: "Mad.Men.S07E06.1080p.rus.LostFilm.TV.mkv",
		CreatedAt:   time.Now().UTC(),
	})

	if err := h.runner.DrainSpool(context.Background()); err != nil {
		t.Fatalf("DrainSpool: %v", err)
	}
	if _, ok := h.state.Lookup("Mad.Men.S07E06.1080p.rus.LostFilm.TV.mkv"); ok {
		t.Error("запись состояния осталась после отчёта - таймаут однажды сработает вхолостую")
	}
}

func TestDrainSpoolTreats404AsSuccess(t *testing.T) {
	h := newHarnessComplete(t, http.StatusNotFound, `{"error":"задание не найдено"}`)
	h.spool.Write(spool.Report{JobID: 42, Status: "ok", CreatedAt: time.Now().UTC()})

	if err := h.runner.DrainSpool(context.Background()); err != nil {
		t.Fatalf("DrainSpool: %v", err)
	}

	entries, _ := h.spool.List()
	if len(entries) != 0 {
		t.Fatal("404 - штатная ситуация (сериал сняли со слежения), запись обязана уйти из спула")
	}
}

func TestDrainSpoolStopsOnUnauthorized(t *testing.T) {
	h := newHarnessComplete(t, http.StatusUnauthorized, `{"error":"нужен Authorization: Bearer <API_TOKEN>"}`)
	h.spool.Write(spool.Report{JobID: 42, Status: "ok", CreatedAt: time.Now().UTC()})
	h.spool.Write(spool.Report{JobID: 43, Status: "ok", CreatedAt: time.Now().UTC().Add(time.Second)})

	err := h.runner.DrainSpool(context.Background())
	if err == nil {
		t.Fatal("ожидалась ошибка авторизации")
	}

	if h.completeCalls != 1 {
		t.Errorf("запросов к /hooks/complete = %d, ожидался 1 — 401 терминален", h.completeCalls)
	}
	entries, _ := h.spool.List()
	if len(entries) != 2 {
		t.Errorf("в спуле %d записей, ожидалось 2 — при 401 отчёты не выбрасываются", len(entries))
	}
}

func TestDrainSpoolKeepsEntryOnServerError(t *testing.T) {
	h := newHarnessComplete(t, http.StatusInternalServerError, `{"error":"внутренняя ошибка"}`)
	h.spool.Write(spool.Report{JobID: 42, Status: "ok", CreatedAt: time.Now().UTC()})

	_ = h.runner.DrainSpool(context.Background())

	entries, _ := h.spool.List()
	if len(entries) != 1 {
		t.Fatal("при 5xx отчёт должен остаться в спуле до следующего цикла")
	}
}

func TestDrainSpoolLogsUnknownTorrent(t *testing.T) {
	h := newHarness(t, `[]`, http.StatusOK)
	h.spool.Write(spool.Report{
		JobID: 0, Status: "ok", ContentName: "Постороннее.mkv",
		CreatedAt: time.Now().UTC(),
	})

	if err := h.runner.DrainSpool(context.Background()); err != nil {
		t.Fatalf("DrainSpool: %v", err)
	}

	if len(h.complete) != 0 {
		t.Error("отчёт без jobId отправлять некуда")
	}
	entries, _ := h.spool.List()
	if len(entries) != 0 {
		t.Error("отчёт по неизвестному торренту должен быть удалён после записи в лог")
	}
}
