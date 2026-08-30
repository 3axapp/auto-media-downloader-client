package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var (
	ErrUnauthorized = errors.New("api: неверный токен")
	ErrNotFound     = errors.New("api: не найдено")
)

// Job - задание из очереди. Поля совпадают со схемой Job в openapi.yaml сервера.
type Job struct {
	ID           int        `json:"id"`
	SeriesID     int        `json:"seriesId"`
	SeriesName   string     `json:"seriesName"`
	SeriesNameEn string     `json:"seriesNameEn"`
	Season       int        `json:"season"`
	Episode      int        `json:"episode"`
	Quality      string     `json:"quality"`
	TorrentName  string     `json:"torrentName"`
	TorrentURL   string     `json:"torrentUrl"`
	LeaseUntil   *time.Time `json:"leaseUntil"`
}

// CompleteRequest - отчёт о результате скачивания.
type CompleteRequest struct {
	JobID  int    `json:"jobId"`
	Status string `json:"status"`
	Path   string `json:"path,omitempty"`
	Error  string `json:"error,omitempty"`
}

type Client struct {
	baseURL string
	token   string
	hc      *http.Client
}

func New(baseURL, token string, timeout time.Duration) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		hc:      &http.Client{Timeout: timeout},
	}
}

func (c *Client) Health(ctx context.Context) error {
	resp, err := c.do(ctx, http.MethodGet, "/health", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return c.checkStatus(resp, http.StatusOK)
}

// Jobs забирает задания в работу. Вызов не идемпотентен: он же лизингует
// задания и увеличивает счётчик attempts на стороне демона.
func (c *Client) Jobs(ctx context.Context, limit int) ([]Job, error) {
	resp, err := c.do(ctx, http.MethodGet, "/jobs?limit="+strconv.Itoa(limit), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := c.checkStatus(resp, http.StatusOK); err != nil {
		return nil, err
	}
	var jobs []Job
	if err := json.NewDecoder(resp.Body).Decode(&jobs); err != nil {
		return nil, fmt.Errorf("разбор списка заданий: %w", err)
	}
	return jobs, nil
}

// Torrent скачивает байты .torrent по пути из поля torrentUrl задания.
func (c *Client) Torrent(ctx context.Context, torrentURL string) ([]byte, error) {
	resp, err := c.do(ctx, http.MethodGet, torrentURL, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := c.checkStatus(resp, http.StatusOK); err != nil {
		return nil, err
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("чтение байтов торрента: %w", err)
	}
	return body, nil
}

// Ack подтверждает получение файла. После него байты на сервере обнуляются.
func (c *Client) Ack(ctx context.Context, id int) error {
	resp, err := c.do(ctx, http.MethodPost, "/jobs/"+strconv.Itoa(id)+"/ack", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return c.checkStatus(resp, http.StatusNoContent)
}

// Complete отчитывается о результате скачивания.
func (c *Client) Complete(ctx context.Context, req CompleteRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("сериализация отчёта: %w", err)
	}
	resp, err := c.do(ctx, http.MethodPost, "/hooks/complete", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return c.checkStatus(resp, http.StatusNoContent)
}

func (c *Client) do(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, fmt.Errorf("сборка запроса %s %s: %w", method, path, err)
	}
	if path != "/health" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("запрос %s %s: %w", method, path, err)
	}
	return resp, nil
}

func (c *Client) checkStatus(resp *http.Response, want int) error {
	if resp.StatusCode == want {
		return nil
	}
	detail := serverError(resp.Body)
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return fmt.Errorf("%w: %s", ErrUnauthorized, detail)
	case http.StatusNotFound:
		return fmt.Errorf("%w: %s", ErrNotFound, detail)
	default:
		return fmt.Errorf("неожиданный ответ %d: %s", resp.StatusCode, detail)
	}
}

// serverError достаёт текст из тела {"error": "…"}; если тело не разбирается,
// возвращает его как есть.
func serverError(r io.Reader) string {
	raw, err := io.ReadAll(io.LimitReader(r, 8<<10))
	if err != nil || len(raw) == 0 {
		return "без описания"
	}
	var payload struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(raw, &payload) == nil && payload.Error != "" {
		return payload.Error
	}
	return strings.TrimSpace(string(raw))
}
