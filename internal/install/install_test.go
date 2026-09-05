package install_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/3axapp/auto-media-downloader-client/internal/install"
)

func TestBatContentsQuotesPaths(t *testing.T) {
	got := install.BatContents(`C:\Program Files\amd-client\amd-client.exe`,
		`C:\ProgramData\amd-client\config.json`)

	if !strings.Contains(got, `"C:\Program Files\amd-client\amd-client.exe"`) {
		t.Errorf("путь к exe не в кавычках - пробел в Program Files сломает вызов:\n%s", got)
	}
	if !strings.Contains(got, `"C:\ProgramData\amd-client\config.json"`) {
		t.Errorf("путь к конфигу не в кавычках:\n%s", got)
	}
}

func TestBatContentsPutsFlagAfterCommand(t *testing.T) {
	got := install.BatContents(`C:\amd\amd-client.exe`, `C:\amd\config.json`)

	doneAt := strings.Index(got, " done")
	flagAt := strings.Index(got, "-config")
	if doneAt < 0 || flagAt < 0 {
		t.Fatalf("в .bat нет команды или флага:\n%s", got)
	}
	if flagAt < doneAt {
		t.Errorf("флаг -config стоит перед командой done - разбор аргументов его не увидит:\n%s", got)
	}
}

func TestWriteBatCreatesFile(t *testing.T) {
	dir := t.TempDir()

	path, err := install.WriteBat(dir, `C:\amd\amd-client.exe`, `C:\amd\config.json`)
	if err != nil {
		t.Fatalf("WriteBat: %v", err)
	}
	if filepath.Base(path) != "torrent-done.bat" {
		t.Errorf("имя файла = %q", filepath.Base(path))
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("файл не создан: %v", err)
	}
	if !strings.Contains(string(body), "done") {
		t.Errorf("в .bat нет команды done:\n%s", body)
	}
}

func TestSettingsHintEscapesBackslashes(t *testing.T) {
	got := install.SettingsHint(`C:\ProgramData\amd-client\torrent-done.bat`)

	if !strings.Contains(got, `C:\\ProgramData\\amd-client\\torrent-done.bat`) {
		t.Errorf("обратные слэши не экранированы - settings.json не разберётся:\n%s", got)
	}
	if !strings.Contains(got, "script-torrent-done-enabled") {
		t.Errorf("нет ключа включения хука:\n%s", got)
	}
}

func TestSettingsHintWarnsAboutRestart(t *testing.T) {
	got := install.SettingsHint(`C:\amd\torrent-done.bat`)

	if !strings.Contains(strings.ToLower(got), "останов") {
		t.Errorf("нет предупреждения о правке при остановленном transmission:\n%s", got)
	}
}
