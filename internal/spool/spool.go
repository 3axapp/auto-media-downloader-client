package spool

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/3axapp/auto-media-downloader-client/internal/atomicfile"
)

const ext = ".json"

// Report - отчёт о результате скачивания.
//
// ContentName - имя контента (TR_TORRENT_NAME), то есть ключ состояния,
// а не имя .torrent-файла.
// JobID = 0 означает, что имя не нашлось в состоянии: событие всё равно
// записывается, чтобы расхождение ключа не потерялось молча.
type Report struct {
	JobID       int       `json:"jobId"`
	Status      string    `json:"status"`
	Path        string    `json:"path,omitempty"`
	Error       string    `json:"error,omitempty"`
	ContentName string    `json:"contentName,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

type Entry struct {
	Path   string
	Report Report
}

func Open(dir string) (*Spool, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("создание каталога спула %s: %w", dir, err)
	}
	return &Spool{dir: dir}, nil
}

type Spool struct {
	dir string
}

func (s *Spool) Write(r Report) (string, error) {
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", fmt.Errorf("сериализация отчёта: %w", err)
	}

	stamp := r.CreatedAt.UTC().Format("20060102T150405.000000000")
	path := filepath.Join(s.dir, fmt.Sprintf("%s-%d%s", stamp, r.JobID, ext))
	for i := 1; ; i++ {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			break
		}
		path = filepath.Join(s.dir, fmt.Sprintf("%s-%d-%d%s", stamp, r.JobID, i, ext))
	}

	if err := atomicfile.WriteFile(path, raw, 0o644); err != nil {
		return "", fmt.Errorf("запись отчёта: %w", err)
	}

	return path, nil
}

func (s *Spool) List() ([]Entry, error) {
	dirEntries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("чтение спула %s: %w", s.dir, err)
	}

	var names []string
	for _, de := range dirEntries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ext) {
			continue
		}
		names = append(names, de.Name())
	}
	sort.Strings(names)

	entries := make([]Entry, 0, len(names))
	for _, name := range names {
		path := filepath.Join(s.dir, name)
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("чтение отчёта %s: %w", path, err)
		}
		var r Report
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, fmt.Errorf("разбор отчёта %s: %w", path, err)
		}
		entries = append(entries, Entry{Path: path, Report: r})
	}
	return entries, nil
}

func (s *Spool) Remove(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("удаление отчёта %s: %w", path, err)
	}
	return nil
}
