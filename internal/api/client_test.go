package api_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/3axapp/auto-media-downloader-client/internal/api"
)

func newClient(t *testing.T, h http.HandlerFunc) *api.Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return api.New(srv.URL, "секрет", 5*time.Second)
}

func TestJobsParsesResponse(t *testing.T) {
	var gotAuth, gotLimit string
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotLimit = r.URL.Query().Get("limit")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"id":42,"seriesId":1136,"seriesName":"Безумцы","seriesNameEn":"Mad Men",
			"season":7,"episode":6,"quality":"MP4",
			"torrentName":"Mad.Men.S07E06.720p.rus.LostFilm.TV.mp4.torrent",
			"torrentUrl":"/jobs/42/torrent","leaseUntil":"2026-08-24T18:40:00Z"}]`))
	})

	jobs, err := c.Jobs(context.Background(), 5)
	if err != nil {
		t.Fatalf("Jobs: %v", err)
	}
	if gotAuth != "Bearer секрет" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotLimit != "5" {
		t.Errorf("limit = %q, ожидалось 5", gotLimit)
	}
	if len(jobs) != 1 {
		t.Fatalf("получено %d заданий, ожидалось 1", len(jobs))
	}
	if jobs[0].ID != 42 || jobs[0].TorrentURL != "/jobs/42/torrent" {
		t.Errorf("задание разобрано неверно: %+v", jobs[0])
	}
	if jobs[0].LeaseUntil == nil || jobs[0].LeaseUntil.UTC().Hour() != 18 {
		t.Errorf("leaseUntil разобран неверно: %v", jobs[0].LeaseUntil)
	}
}

func TestJobsHandlesNullLeaseUntil(t *testing.T) {
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"id":1,"seriesId":1,"seriesName":"","seriesNameEn":"","season":1,
			"episode":1,"quality":"SD","torrentName":"a.torrent","torrentUrl":"/jobs/1/torrent",
			"leaseUntil":null}]`))
	})

	jobs, err := c.Jobs(context.Background(), 1)
	if err != nil {
		t.Fatalf("Jobs: %v", err)
	}
	if jobs[0].LeaseUntil != nil {
		t.Errorf("LeaseUntil = %v, ожидался nil", jobs[0].LeaseUntil)
	}
}

func TestJobsUnauthorized(t *testing.T) {
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"нужен Authorization: Bearer <API_TOKEN>"}`))
	})

	_, err := c.Jobs(context.Background(), 5)
	if !errors.Is(err, api.ErrUnauthorized) {
		t.Fatalf("ошибка = %v, ожидалась ErrUnauthorized", err)
	}
}

func TestTorrentReturnsBytes(t *testing.T) {
	var gotPath string
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/x-bittorrent")
		w.Write([]byte("d8:announce…e"))
	})

	body, err := c.Torrent(context.Background(), "/jobs/42/torrent")
	if err != nil {
		t.Fatalf("Torrent: %v", err)
	}
	if gotPath != "/jobs/42/torrent" {
		t.Errorf("путь = %q", gotPath)
	}
	if string(body) != "d8:announce…e" {
		t.Errorf("тело = %q", body)
	}
}

func TestTorrentNotFound(t *testing.T) {
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"байты торрента недоступны"}`))
	})

	_, err := c.Torrent(context.Background(), "/jobs/42/torrent")
	if !errors.Is(err, api.ErrNotFound) {
		t.Fatalf("ошибка = %v, ожидалась ErrNotFound", err)
	}
}

func TestAckSuccess(t *testing.T) {
	var gotMethod, gotPath string
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})

	if err := c.Ack(context.Background(), 42); err != nil {
		t.Fatalf("Ack: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/jobs/42/ack" {
		t.Errorf("запрос = %s %s", gotMethod, gotPath)
	}
}

func TestCompleteSendsBody(t *testing.T) {
	var body []byte
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		body = make([]byte, r.ContentLength)
		r.Body.Read(body)
		w.WriteHeader(http.StatusNoContent)
	})

	err := c.Complete(context.Background(), api.CompleteRequest{
		JobID: 42, Status: "ok", Path: "/media/S07E06.mkv",
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if !bytesContains(body, `"jobId":42`) || !bytesContains(body, `"status":"ok"`) {
		t.Errorf("тело запроса = %s", body)
	}
}

func TestCompleteNotFoundIsRecognisable(t *testing.T) {
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"задание не найдено"}`))
	})

	err := c.Complete(context.Background(), api.CompleteRequest{JobID: 42, Status: "ok"})
	if !errors.Is(err, api.ErrNotFound) {
		t.Fatalf("ошибка = %v, ожидалась ErrNotFound", err)
	}
}

func TestErrorTextFromServerIsPreserved(t *testing.T) {
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"ожидались поля jobId и status (ok|error)"}`))
	})

	err := c.Complete(context.Background(), api.CompleteRequest{JobID: 1, Status: "ok"})
	if err == nil || !bytesContains([]byte(err.Error()), "ожидались поля jobId") {
		t.Fatalf("ошибка = %v, ожидался текст сервера", err)
	}
}

func bytesContains(haystack []byte, needle string) bool {
	return len(needle) == 0 || len(haystack) >= len(needle) &&
		func() bool {
			for i := 0; i+len(needle) <= len(haystack); i++ {
				if string(haystack[i:i+len(needle)]) == needle {
					return true
				}
			}
			return false
		}()
}
