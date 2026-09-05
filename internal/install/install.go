package install

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/3axapp/auto-media-downloader-client/internal/atomicfile"
)

const BatName = "torrent-done.bat"

func BatContents(exePath, configPath string) string {
	return fmt.Sprintf("@echo off\r\n\"%s\" done -config \"%s\"\r\n", exePath, configPath)
}

func WriteBat(dir, exePath, configPath string) (string, error) {
	path := filepath.Join(dir, BatName)
	if err := atomicfile.WriteFile(path, []byte(BatContents(exePath, configPath)), 0o755); err != nil {
		return "", fmt.Errorf("запись %s: %w", path, err)
	}
	return path, nil
}

func SettingsHint(batPath string) string {
	quoted, _ := json.Marshal(batPath)
	return fmt.Sprintf(`Настройте transmission — при ОСТАНОВЛЕННОМ transmission впишите
в settings.json:

    "script-torrent-done-enabled": true,
    "script-torrent-done-filename": %s

Если transmission запущен, он перезапишет файл своими настройками при выходе.`, quoted)
}
