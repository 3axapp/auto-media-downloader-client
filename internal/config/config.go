package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"
)

type Duration time.Duration

func (d *Duration) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("длительность должна быть строкой вида \"5m\": %w", err)
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("не разбирается длительность %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

func (d Duration) MarshalJSON() (data []byte, err error) {
	return json.Marshal(time.Duration(d).String())
}

type Config struct {
	BaseURL         string   `json:"base_url"`
	APIToken        string   `json:"api_token"`
	WatchDir        string   `json:"watch_dir"`
	PollInterval    Duration `json:"poll_interval"`
	JobsPerPoll     int      `json:"jobs_per_poll"`
	DownloadTimeout Duration `json:"download_timeout"`
	HTTPTimeout     Duration `json:"http_timeout"`
	LogLevel        string   `json:"log_level"`

	Dir string `json:"-"`
}

func DefaultPath() string {
	if runtime.GOOS == "windows" {
		base := os.Getenv("ProgramData")
		if base == "" {
			base = `C:\ProgramData`
		}
		return filepath.Join(base, "amd-client", "config.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".config", "amd-client", "config.json")
}

func (c *Config) StatePath() string { return filepath.Join(c.Dir, "state.json") }
func (c *Config) SpoolDir() string  { return filepath.Join(c.Dir, "spool") }
func (c *Config) LogPath() string   { return filepath.Join(c.Dir, "logs", "amd-client.log") }

// TmpDir лежит внутри watch-папки намеренно: rename атомарен только в
// пределах одного тома, а каталог данных может оказаться на другом диске.
func (c *Config) TmpDir() string { return filepath.Join(c.WatchDir, ".amd-tmp") }

// Load читает конфиг, накладывает AMD_*-оверрайды, дефолты и валидирует результат.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("чтение конфига %s: %w", path, err)
	}

	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("разбор конфига %s: %w", path, err)
	}
	cfg.Dir = filepath.Dir(path)

	if err := cfg.applyEnv(); err != nil {
		return nil, err
	}
	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) applyEnv() error {
	if v := os.Getenv("AMD_BASE_URL"); v != "" {
		c.BaseURL = v
	}
	if v := os.Getenv("AMD_API_TOKEN"); v != "" {
		c.APIToken = v
	}
	if v := os.Getenv("AMD_WATCH_DIR"); v != "" {
		c.WatchDir = v
	}
	if v := os.Getenv("AMD_LOG_LEVEL"); v != "" {
		c.LogLevel = v
	}
	if v := os.Getenv("AMD_JOBS_PER_POLL"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("AMD_JOBS_PER_POLL=%q не число: %w", v, err)
		}
		c.JobsPerPoll = n
	}
	for _, o := range []struct {
		env string
		dst *Duration
	}{
		{"AMD_POLL_INTERVAL", &c.PollInterval},
		{"AMD_DOWNLOAD_TIMEOUT", &c.DownloadTimeout},
		{"AMD_HTTP_TIMEOUT", &c.HTTPTimeout},
	} {
		v := os.Getenv(o.env)
		if v == "" {
			continue
		}
		parsed, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("%s=%q не разбирается как длительность: %w", o.env, v, err)
		}
		*o.dst = Duration(parsed)
	}
	return nil
}

func (c *Config) applyDefaults() {
	if c.PollInterval == 0 {
		c.PollInterval = Duration(5 * time.Minute)
	}
	if c.DownloadTimeout == 0 {
		c.DownloadTimeout = Duration(72 * time.Hour)
	}
	if c.HTTPTimeout == 0 {
		c.HTTPTimeout = Duration(30 * time.Second)
	}
	if c.LogLevel == "" {
		c.LogLevel = "info"
	}

	switch {
	case c.JobsPerPoll <= 0:
		c.JobsPerPoll = 5
	case c.JobsPerPoll > 100:
		c.JobsPerPoll = 100
	}
}

func (c *Config) validate() error {
	if c.BaseURL == "" {
		return fmt.Errorf("не задан base_url (или AMD_BASE_URL)")
	}
	if c.APIToken == "" {
		return fmt.Errorf("не задан api_token (или AMD_API_TOKEN)")
	}
	if c.WatchDir == "" {
		return fmt.Errorf("не задан watch_dir (или AMD_WATCH_DIR)")
	}
	return nil
}
