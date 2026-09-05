package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/3axapp/auto-media-downloader-client/internal/api"
	"github.com/3axapp/auto-media-downloader-client/internal/config"
	"github.com/3axapp/auto-media-downloader-client/internal/hook"
	"github.com/3axapp/auto-media-downloader-client/internal/install"
	"github.com/3axapp/auto-media-downloader-client/internal/logging"
	"github.com/3axapp/auto-media-downloader-client/internal/runner"
	"github.com/3axapp/auto-media-downloader-client/internal/spool"
	"github.com/3axapp/auto-media-downloader-client/internal/state"
	"github.com/3axapp/auto-media-downloader-client/internal/winsvc"
)

// ServiceName — имя службы в SCM.
const ServiceName = "auto-media-downloader-client"

// version подставляется линкером: -X main.version=…
var version = "dev"

func main() {
	os.Exit(dispatch(os.Args[1:], os.Stderr))
}

func dispatch(args []string, w io.Writer) int {
	if len(args) == 0 {
		usage(w)
		return 2
	}

	command, rest := args[0], args[1:]
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(w)
	configPath := fs.String("config", config.DefaultPath(), "путь к config.json")
	if err := fs.Parse(rest); err != nil {
		return 2
	}

	switch command {
	case "version":
		fmt.Fprintf(w, "amd-client %s\n", version)
		return 0
	case "run":
		return cmdRun(*configPath, w)
	case "done":
		return cmdDone(*configPath, w)
	case "check":
		return cmdCheck(*configPath, w)
	case "install":
		return cmdInstall(*configPath, w)
	case "uninstall":
		return cmdUninstall(w)
	default:
		fmt.Fprintf(w, "неизвестная команда %q\n\n", command)
		usage(w)
		return 2
	}
}

func usage(w io.Writer) {
	fmt.Fprintf(w, `amd-client %s — скачивающий клиент auto-media-downloader

Использование: amd-client <команда> [флаги]

Команды:
  run        рабочий цикл: опрос заданий, выгрузка торрентов, отправка отчётов
  done       хук transmission: записать отчёт о завершении закачки
  check      проверить конфиг, связь с демоном и права на запись
  install    зарегистрировать службу Windows и создать torrent-done.bat
  uninstall  удалить службу Windows
  version    показать версию

Общий флаг: -config <путь к config.json> (по умолчанию %s)
`, version, config.DefaultPath())
}

func cmdRun(configPath string, w io.Writer) int {
	asService := winsvc.IsService()

	_, r, closer, err := build(configPath, !asService)
	if err != nil {
		fmt.Fprintf(w, "запуск невозможен: %v\n", err)
		return 1
	}
	defer closer.Close()

	if asService {
		if err := winsvc.Run(ServiceName, r.Run); err != nil {
			fmt.Fprintf(w, "служба завершилась с ошибкой: %v\n", err)
			return 1
		}
		return 0
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := r.Run(ctx); err != nil {
		fmt.Fprintf(w, "цикл завершился с ошибкой: %v\n", err)
		return 1
	}
	return 0
}

func build(configPath string, console bool) (*config.Config, *runner.Runner, io.Closer, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, nil, nil, err
	}
	log, closer, err := logging.New(cfg.LogPath(), cfg.LogLevel, console)
	if err != nil {
		return nil, nil, nil, err
	}
	st, err := state.Open(cfg.StatePath())
	if err != nil {
		closer.Close()
		return nil, nil, nil, err
	}
	sp, err := spool.Open(cfg.SpoolDir())
	if err != nil {
		closer.Close()
		return nil, nil, nil, err
	}
	client := api.New(cfg.BaseURL, cfg.APIToken, time.Duration(cfg.HTTPTimeout))
	return cfg, runner.New(cfg, client, st, sp, log), closer, nil
}

func cmdDone(configPath string, w io.Writer) int {
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(w, "отчёт не записан: %v\n", err)
		return 1
	}
	report, err := hook.Run(cfg, os.Getenv, time.Now())
	if err != nil {
		fmt.Fprintf(w, "отчёт не записан: %v\n", err)
		return 1
	}
	fmt.Fprintf(w, "отчёт поставлен в очередь: задание %d, торрент %s\n",
		report.JobID, report.ContentName)
	return 0
}

func cmdCheck(configPath string, w io.Writer) int {
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(w, "конфиг: %v\n", err)
		return 1
	}
	fmt.Fprintf(w, "конфиг: %s\n", configPath)

	ok := true
	if err := os.MkdirAll(cfg.TmpDir(), 0o755); err != nil {
		fmt.Fprintf(w, "watch-папка %s: %v\n", cfg.WatchDir, err)
		ok = false
	} else {
		fmt.Fprintf(w, "watch-папка %s: доступна на запись\n", cfg.WatchDir)
	}
	if _, err := spool.Open(cfg.SpoolDir()); err != nil {
		fmt.Fprintf(w, "каталог данных %s: %v\n", cfg.Dir, err)
		ok = false
	} else {
		fmt.Fprintf(w, "каталог данных %s: доступен на запись\n", cfg.Dir)
	}

	client := api.New(cfg.BaseURL, cfg.APIToken, time.Duration(cfg.HTTPTimeout))
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.HTTPTimeout))
	defer cancel()
	if err := client.Health(ctx); err != nil {
		fmt.Fprintf(w, "демон %s: %v\n", cfg.BaseURL, err)
		ok = false
	} else {
		fmt.Fprintf(w, "демон %s: отвечает\n", cfg.BaseURL)
	}
	// Демон проверяет авторизацию до маршрутизации, поэтому неверный
	// токен даёт 401, а верный — 404 «байты торрента недоступны».
	_, tokenErr := client.Torrent(ctx, "/jobs/999999999/torrent")
	switch err := tokenErr; {
	case errors.Is(err, api.ErrNotFound):
		fmt.Fprintf(w, "токен принят\n")
	case errors.Is(err, api.ErrUnauthorized):
		fmt.Fprintf(w, "токен отвергнут демоном: %v\n", err)
		ok = false
	case err != nil:
		fmt.Fprintf(w, "внимание: проверка токена не удалась: %v\n", err)
		ok = false
	default:
		fmt.Fprintf(w, "внимание: демон отдал торрент несуществующего задания\n")
		ok = false
	}

	if !ok {
		return 1
	}
	fmt.Fprintln(w, "всё в порядке")
	return 0
}

func cmdInstall(configPath string, w io.Writer) int {
	if err := install.Install(ServiceName, configPath, w); err != nil {
		fmt.Fprintf(w, "установка не удалась: %v\n", err)
		return 1
	}
	return 0
}

func cmdUninstall(w io.Writer) int {
	if err := install.Uninstall(ServiceName, w); err != nil {
		fmt.Fprintf(w, "удаление не удалось: %v\n", err)
		return 1
	}
	return 0
}
