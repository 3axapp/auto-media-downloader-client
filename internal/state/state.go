package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/3axapp/auto-media-downloader-client/internal/atomicfile"
)

type Entry struct {
	JobID       int       `json:"jobId"`
	TorrentName string    `json:"torrentName"`
	EnqueuedAt  time.Time `json:"enqueuedAt"`
}

type Named struct {
	ContentName string
	Entry       Entry
}

// ContentName переводит имя .torrent-файла в имя контента, которое
// transmission передаёт хуку в TR_TORRENT_NAME.
// Работает потому, что раздачи LostFilm однофайловые: для однофайлового
// торрента info.name и есть имя файла, а .torrent называется так же плюс
// расширение. Многофайловая раздача этот ключ сломает.
func ContentName(torrentName string) string {
	return strings.TrimSuffix(torrentName, ".torrent")
}

func Open(path string) (*Store, error) {
	s := &Store{path: path, entries: map[string]Entry{}}

	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("чтение состояния %s: %w", path, err)
	}
	if len(raw) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(raw, &s.entries); err != nil {
		return nil, fmt.Errorf("разбор состояния %s: %w", path, err)
	}
	return s, nil
}

type Store struct {
	path    string
	mu      sync.Mutex
	entries map[string]Entry
}

func (s *Store) Put(contentName string, e Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[contentName] = e
	return s.flush()
}

func (s *Store) Lookup(contentName string) (Entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[contentName]

	return e, ok
}

func (s *Store) Delete(contentName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, contentName)
	return s.flush()
}

func (s *Store) OlderThan(d time.Duration, now time.Time) []Named {
	s.mu.Lock()
	defer s.mu.Unlock()

	var cutoff = now.Add(-d)
	var stale []Named
	for name, e := range s.entries {
		if e.EnqueuedAt.Before(cutoff) {
			stale = append(stale, Named{ContentName: name, Entry: e})
		}
	}
	return stale
}

func (s *Store) flush() error {
	raw, err := json.MarshalIndent(s.entries, "", "  ")
	if err != nil {
		return fmt.Errorf("сериализация состояния: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("создание каталога состояния: %w", err)
	}
	if err := atomicfile.WriteFile(s.path, raw, 0o644); err != nil {
		return fmt.Errorf("запись состояния: %w", err)
	}
	return nil
}
