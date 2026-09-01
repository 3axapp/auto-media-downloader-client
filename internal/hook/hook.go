// Package hook - подкоманда done, которую вызывает transmission по
// завершении закачки.
//
// Процесс живёт миллисекунды и в сеть не ходит: он только кладёт отчёт в
// спул. Доставку с ретраями делает служба - иначе моргнувшая сеть до
// демона теряла бы отчёт навсегда, а transmission залипал бы на таймауте.
package hook

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/3axapp/auto-media-downloader-client/internal/config"
	"github.com/3axapp/auto-media-downloader-client/internal/spool"
	"github.com/3axapp/auto-media-downloader-client/internal/state"
)

func Run(cfg *config.Config, env func(string) string, now time.Time) (spool.Report, error) {
	torrentName := env("TR_TORRENT_NAME")
	if torrentName == "" {
		return spool.Report{}, fmt.Errorf("не задан TR_TORRENT_NAME: команда done вызывается transmission, а не руками")
	}

	report := spool.Report{
		Status:      "ok",
		Path:        filepath.Join(env("TR_TORRENT_DIR"), torrentName),
		ContentName: torrentName,
		CreatedAt:   now.UTC(),
	}

	st, err := state.Open(cfg.StatePath())
	if err != nil {
		return spool.Report{}, err
	}
	if entry, ok := st.Lookup(torrentName); ok {
		report.JobID = entry.JobID
	}

	sp, err := spool.Open(cfg.SpoolDir())
	if err != nil {
		return spool.Report{}, err
	}
	if _, err := sp.Write(report); err != nil {
		return spool.Report{}, err
	}
	return report, nil
}
