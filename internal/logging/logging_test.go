package logging_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/3axapp/auto-media-downloader-client/internal/logging"
)

func TestWriterCreatesFileAndDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "amd-client.log")

	w, err := logging.NewWriter(path, 1024, 2)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	defer w.Close()

	if _, err := w.Write([]byte("строка\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "строка\n" {
		t.Errorf("содержимое = %q", got)
	}
}

func TestWriterRotatesBySize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "amd-client.log")

	w, err := logging.NewWriter(path, 32, 2)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	defer w.Close()

	for i := 0; i < 20; i++ {
		if _, err := w.Write([]byte("двадцать байт ровно-ровно\n")); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	entries, _ := os.ReadDir(dir)
	var rotated int
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "amd-client.log.") {
			rotated++
		}
	}
	if rotated == 0 {
		t.Fatal("ротации не произошло")
	}
	if rotated > 2 {
		t.Fatalf("сохранено %d старых файлов, ожидалось не больше 2", rotated)
	}
}

func TestNewLoggerWritesToFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "amd-client.log")

	log, closer, err := logging.New(path, "info", false)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	log.Info("задание выдано", "jobId", 42)
	closer.Close()

	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "задание выдано") || !strings.Contains(string(got), "42") {
		t.Fatalf("в логе нет записи: %s", got)
	}
}

func TestNewLoggerRespectsLevel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "amd-client.log")

	log, closer, err := logging.New(path, "warn", false)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	log.Info("это не должно попасть в лог")
	log.Warn("а это должно")
	closer.Close()

	got, _ := os.ReadFile(path)
	if strings.Contains(string(got), "не должно попасть") {
		t.Errorf("info просочился при уровне warn: %s", got)
	}
	if !strings.Contains(string(got), "а это должно") {
		t.Errorf("warn не попал в лог: %s", got)
	}
}
