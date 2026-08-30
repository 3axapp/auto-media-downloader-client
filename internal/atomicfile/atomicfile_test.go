package atomicfile_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/3axapp/auto-media-downloader-client/internal/atomicfile"
)

func TestWriteFileCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	if err := atomicfile.WriteFile(path, []byte("привет"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "привет" {
		t.Fatalf("содержимое = %q, ожидалось %q", got, "привет")
	}
}

func TestWriteFileOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	if err := atomicfile.WriteFile(path, []byte("первое"), 0o644); err != nil {
		t.Fatalf("первая запись: %v", err)
	}
	if err := atomicfile.WriteFile(path, []byte("второе"), 0o644); err != nil {
		t.Fatalf("вторая запись: %v", err)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "второе" {
		t.Fatalf("содержимое = %q, ожидалось %q", got, "второе")
	}
}

func TestWriteFileLeavesNoTempOnSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	if err := atomicfile.WriteFile(path, []byte("данные"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("в каталоге %d файлов (%s), ожидался ровно один", len(entries), strings.Join(names, ", "))
	}
}

func TestWriteFileLeavesNoTempOnFailure(t *testing.T) {
	dir := t.TempDir()
	// Путь внутри несуществующего каталога: rename обязан упасть.
	path := filepath.Join(dir, "нет-такого-каталога", "state.json")

	if err := atomicfile.WriteFile(path, []byte("данные"), 0o644); err == nil {
		t.Fatal("ожидалась ошибка записи в несуществующий каталог")
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("после неудачи в каталоге остались файлы: %v", entries)
	}
}
