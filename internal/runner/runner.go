package runner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/3axapp/auto-media-downloader-client/internal/api"
	"github.com/3axapp/auto-media-downloader-client/internal/config"
	"github.com/3axapp/auto-media-downloader-client/internal/spool"
	"github.com/3axapp/auto-media-downloader-client/internal/state"
)

func New(cfg *config.Config, client *api.Client, st *state.Store, sp *spool.Spool, log *slog.Logger) *Runner {
	return &Runner{
		cfg:   cfg,
		api:   client,
		state: st,
		spool: sp,
		log:   log,
		Now:   time.Now,
	}
}

// validTorrentName защищает от имени из сети, уводящего запись за пределы
// watch-папки.
func validTorrentName(name string) error {
	if name == "" {
		return fmt.Errorf("пустое имя торрента")
	}
	if name != filepath.Base(name) || name == "." || name == ".." {
		return fmt.Errorf("недопустимое имя торрента %q", name)
	}
	return nil
}

type Runner struct {
	cfg   *config.Config
	api   *api.Client
	state *state.Store
	spool *spool.Spool
	log   *slog.Logger
	// Now подменяется в тестах.
	Now func() time.Time
}

func (r *Runner) FetchJobs(ctx context.Context) error {
	jobs, err := r.api.Jobs(ctx, r.cfg.JobsPerPoll)
	if err != nil {
		return fmt.Errorf("получение заданий: %w", err)
	}
	for _, job := range jobs {
		if err := r.fetchOne(ctx, job); err != nil {
			r.log.Error("задание не выгружено",
				"jobId", job.ID, "торрент", job.TorrentName, "ошибка", err)
		}
	}
	return nil
}
func (r *Runner) fetchOne(ctx context.Context, job api.Job) error {
	if err := validTorrentName(job.TorrentName); err != nil {
		return err
	}

	body, err := r.api.Torrent(ctx, job.TorrentURL)
	if err != nil {
		return fmt.Errorf("скачивание торрента: %w", err)
	}

	contentName := state.ContentName(job.TorrentName)
	entry := state.Entry{
		JobID:       job.ID,
		TorrentName: job.TorrentName,
		EnqueuedAt:  r.Now().UTC(),
	}
	if err := r.state.Put(contentName, entry); err != nil {
		return err
	}

	if err := r.placeTorrent(job.TorrentName, body); err != nil {
		if delErr := r.state.Delete(contentName); delErr != nil {
			r.log.Error("не удалось убрать запись состояния", "ошибка", delErr)
		}
		return err
	}

	if err := r.api.Ack(ctx, job.ID); err != nil {
		return fmt.Errorf("подтверждение получения: %w", err)
	}

	r.log.Info("торрент передан transmission",
		"jobId", job.ID, "торрент", job.TorrentName,
		"сериал", job.SeriesName, "сезон", job.Season, "серия", job.Episode)
	return nil
}

func (r *Runner) placeTorrent(torrentName string, body []byte) error {
	if err := os.MkdirAll(r.cfg.WatchDir, 0o755); err != nil {
		return fmt.Errorf("создание watch-папки %s: %w", r.cfg.WatchDir, err)
	}
	tmpDir := r.cfg.TmpDir()
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return fmt.Errorf("создание временного каталога %s: %w", tmpDir, err)
	}

	tmp, err := os.CreateTemp(tmpDir, "*.torrent.part")
	if err != nil {
		return fmt.Errorf("создание временного файла: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() {
		tmp.Close()
		os.Remove(tmpName)
	}

	if _, err := tmp.Write(body); err != nil {
		cleanup()
		return fmt.Errorf("запись торрента: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("сброс торрента на диск: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("закрытие временного файла: %w", err)
	}

	target := filepath.Join(r.cfg.WatchDir, torrentName)
	if err := os.Rename(tmpName, target); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("перенос торрента в %s: %w", target, err)
	}
	return nil
}

// DrainSpool доставляет накопленные отчёты демону.
// Три исхода обрабатываются по-разному:
//   - 404 - успех: задание сняли со слежения, отчёт учитывать некуда;
//   - 401 - терминально: ошибка конфигурации, дальнейшие попытки бессмысленны;
//   - прочие ошибки - отчёт остаётся в спуле до следующего цикла.
func (r *Runner) DrainSpool(ctx context.Context) error {
	entries, err := r.spool.List()
	if err != nil {
		return err
	}

	for _, entry := range entries {
		report := entry.Report

		if report.JobID == 0 {
			r.log.Warn("отчёт по неизвестному торренту: имя не нашлось в состоянии",
				"торрент", report.ContentName, "путь", report.Path)
			if err := r.spool.Remove(entry.Path); err != nil {
				r.log.Error("не удалось удалить отчёт", "файл", entry.Path, "ошибка", err)
			}
			continue
		}

		err := r.api.Complete(ctx, api.CompleteRequest{
			JobID:  report.JobID,
			Status: report.Status,
			Path:   report.Path,
			Error:  report.Error,
		})
		switch {
		case err == nil:
			r.log.Info("отчёт доставлен", "jobId", report.JobID, "статус", report.Status)
		case errors.Is(err, api.ErrNotFound):
			r.log.Info("задание уже снято со слежения, отчёт учитывать некуда",
				"jobId", report.JobID)
		case errors.Is(err, api.ErrUnauthorized):
			r.log.Error("отчёты не доставляются: неверный токен, проверьте api_token",
				"jobId", report.JobID)
			return fmt.Errorf("доставка отчётов остановлена: %w", err)
		default:
			r.log.Warn("отчёт не доставлен, попробуем в следующем цикле",
				"jobId", report.JobID, "ошибка", err)
			continue
		}

		if err := r.spool.Remove(entry.Path); err != nil {
			r.log.Error("не удалось удалить отчёт", "файл", entry.Path, "ошибка", err)
			continue
		}
		if report.ContentName != "" {
			if err := r.state.Delete(report.ContentName); err != nil {
				r.log.Error("не удалось убрать запись состояния",
					"торрент", report.ContentName, "ошибка", err)
			}
		}
	}
	return nil
}

// ExpireStale отчитывается об ошибке по закачкам, о которых transmission
// молчит дольше download_timeout.
// Отчёт кладётся в спул, а не отправляется напрямую: путь доставки один
// на все отчёты.
func (r *Runner) ExpireStale(ctx context.Context) error {
	timeout := time.Duration(r.cfg.DownloadTimeout)
	stale := r.state.OlderThan(timeout, r.Now())

	for _, item := range stale {
		report := spool.Report{
			JobID:       item.Entry.JobID,
			Status:      "error",
			ContentName: item.ContentName,
			Error: fmt.Sprintf("закачка не завершилась за %s (поставлена в очередь %s)",
				timeout, item.Entry.EnqueuedAt.Format(time.RFC3339)),
			CreatedAt: r.Now().UTC(),
		}
		if _, err := r.spool.Write(report); err != nil {
			r.log.Error("не удалось записать отчёт о просрочке",
				"jobId", report.JobID, "ошибка", err)
			continue
		}
		if err := r.state.Delete(item.ContentName); err != nil {
			r.log.Error("не удалось убрать просроченную запись",
				"торрент", item.ContentName, "ошибка", err)
			continue
		}
		r.log.Warn("закачка не завершилась в срок",
			"jobId", report.JobID, "торрент", item.ContentName, "срок", timeout)
	}
	return nil
}

// Tick - один проход цикла. Сначала разбираемся с накопленным, потом
// берём новое: GET /jobs сжигает попытки, и незачем набирать заданий,
// пока не разгребли старые отчёты.
func (r *Runner) Tick(ctx context.Context) {
	if err := r.ExpireStale(ctx); err != nil {
		r.log.Error("проверка просроченных закачек", "ошибка", err)
	}
	if err := r.DrainSpool(ctx); err != nil {
		r.log.Error("доставка отчётов", "ошибка", err)
	}
	if err := r.FetchJobs(ctx); err != nil {
		r.log.Error("получение заданий", "ошибка", err)
	}
}

// Run крутит цикл до отмены контекста. Первый проход - сразу, не дожидаясь
// первого тика: служба должна забрать накопившееся при старте.
func (r *Runner) Run(ctx context.Context) error {
	interval := time.Duration(r.cfg.PollInterval)
	r.log.Info("клиент запущен",
		"демон", r.cfg.BaseURL, "watch", r.cfg.WatchDir, "интервал", interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	r.Tick(ctx)
	for {
		select {
		case <-ctx.Done():
			r.log.Info("клиент остановлен")
			return nil
		case <-ticker.C:
			r.Tick(ctx)
		}
	}
}
