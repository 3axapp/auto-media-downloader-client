package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/3axapp/auto-media-downloader-client/internal/config"
)

func write(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("подготовка конфига: %v", err)
	}
	return path
}

func TestLoadAppliesDefaults(t *testing.T) {
	path := write(t, `{"base_url":"http://nas:8080","api_token":"t","watch_dir":"/w"}`)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if time.Duration(cfg.PollInterval) != 5*time.Minute {
		t.Errorf("PollInterval = %v, ожидалось 5m", time.Duration(cfg.PollInterval))
	}
	if cfg.JobsPerPoll != 5 {
		t.Errorf("JobsPerPoll = %d, ожидалось 5", cfg.JobsPerPoll)
	}
	if time.Duration(cfg.DownloadTimeout) != 72*time.Hour {
		t.Errorf("DownloadTimeout = %v, ожидалось 72h", time.Duration(cfg.DownloadTimeout))
	}
	if time.Duration(cfg.HTTPTimeout) != 30*time.Second {
		t.Errorf("HTTPTimeout = %v, ожидалось 30s", time.Duration(cfg.HTTPTimeout))
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, ожидалось info", cfg.LogLevel)
	}
}

func TestLoadParsesDurations(t *testing.T) {
	path := write(t, `{"base_url":"http://nas:8080","api_token":"t","watch_dir":"/w",
		"poll_interval":"90s","download_timeout":"12h","http_timeout":"5s"}`)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if time.Duration(cfg.PollInterval) != 90*time.Second {
		t.Errorf("PollInterval = %v, ожидалось 90s", time.Duration(cfg.PollInterval))
	}
	if time.Duration(cfg.DownloadTimeout) != 12*time.Hour {
		t.Errorf("DownloadTimeout = %v, ожидалось 12h", time.Duration(cfg.DownloadTimeout))
	}
}

func TestLoadEnvOverridesFile(t *testing.T) {
	path := write(t, `{"base_url":"http://nas:8080","api_token":"из-файла","watch_dir":"/w"}`)
	t.Setenv("AMD_API_TOKEN", "из-окружения")
	t.Setenv("AMD_JOBS_PER_POLL", "9")
	t.Setenv("AMD_POLL_INTERVAL", "1m")

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.APIToken != "из-окружения" {
		t.Errorf("APIToken = %q, ожидалось из-окружения", cfg.APIToken)
	}
	if cfg.JobsPerPoll != 9 {
		t.Errorf("JobsPerPoll = %d, ожидалось 9", cfg.JobsPerPoll)
	}
	if time.Duration(cfg.PollInterval) != time.Minute {
		t.Errorf("PollInterval = %v, ожидалось 1m", time.Duration(cfg.PollInterval))
	}
}

func TestLoadClampsJobsPerPoll(t *testing.T) {
	path := write(t, `{"base_url":"http://nas:8080","api_token":"t","watch_dir":"/w","jobs_per_poll":500}`)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.JobsPerPoll != 100 {
		t.Errorf("JobsPerPoll = %d, ожидалось зажатие до 100", cfg.JobsPerPoll)
	}
}

func TestLoadRejectsMissingRequiredFields(t *testing.T) {
	cases := map[string]string{
		"без base_url":  `{"api_token":"t","watch_dir":"/w"}`,
		"без api_token": `{"base_url":"http://nas:8080","watch_dir":"/w"}`,
		"без watch_dir": `{"base_url":"http://nas:8080","api_token":"t"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := config.Load(write(t, body)); err == nil {
				t.Fatal("ожидалась ошибка валидации")
			}
		})
	}
}

func TestPathsDeriveFromConfigDir(t *testing.T) {
	path := write(t, `{"base_url":"http://nas:8080","api_token":"t","watch_dir":"/w"}`)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	dir := filepath.Dir(path)
	if cfg.StatePath() != filepath.Join(dir, "state.json") {
		t.Errorf("StatePath = %q", cfg.StatePath())
	}
	if cfg.SpoolDir() != filepath.Join(dir, "spool") {
		t.Errorf("SpoolDir = %q", cfg.SpoolDir())
	}
	if cfg.LogPath() != filepath.Join(dir, "logs", "amd-client.log") {
		t.Errorf("LogPath = %q", cfg.LogPath())
	}
	if cfg.TmpDir() != filepath.Join("/w", ".amd-tmp") {
		t.Errorf("TmpDir = %q, ожидался .amd-tmp внутри watch_dir", cfg.TmpDir())
	}
}
