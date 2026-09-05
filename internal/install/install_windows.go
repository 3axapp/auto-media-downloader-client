//go:build windows

package install

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"
)

// Install регистрирует службу, создаёт каталоги и кладёт обёртку для
// transmission.
func Install(serviceName string, configPath string, w io.Writer) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("определение пути к исполняемому файлу: %w", err)
	}
	exePath, err = filepath.Abs(exePath)
	if err != nil {
		return fmt.Errorf("абсолютный путь к исполняемому файлу: %w", err)
	}
	configPath, err = filepath.Abs(configPath)
	if err != nil {
		return fmt.Errorf("абсолютный путь к конфигу: %w", err)
	}

	dataDir := filepath.Dir(configPath)
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("создание каталога данных %s: %w", dataDir, err)
	}
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return fmt.Errorf("нет конфига %s — создайте его перед установкой (образец в README)", configPath)
	}

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("подключение к диспетчеру служб (запустите от администратора): %w", err)
	}
	defer m.Disconnect()

	if existing, err := m.OpenService(serviceName); err == nil {
		existing.Close()
		return fmt.Errorf("служба %s уже установлена — сначала выполните uninstall", serviceName)
	}

	service, err := m.CreateService(serviceName, exePath, mgr.Config{
		DisplayName:  "auto-media-downloader client",
		Description:  "Забирает задания демона LostFilm и кладёт .torrent в watch-папку transmission",
		StartType:    mgr.StartAutomatic,
		ErrorControl: mgr.ErrorNormal,
	}, "run", "-config", configPath)
	if err != nil {
		return fmt.Errorf("создание службы %s: %w", serviceName, err)
	}
	defer service.Close()

	if err := service.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 60_000_000_000},
		{Type: mgr.ServiceRestart, Delay: 60_000_000_000},
		{Type: mgr.ServiceRestart, Delay: 60_000_000_000},
	}, 86400); err != nil {
		fmt.Fprintf(w, "внимание: не удалось настроить перезапуск после сбоя: %v\n", err)
	}

	if err := eventlog.InstallAsEventCreate(serviceName, eventlog.Error|eventlog.Warning|eventlog.Info); err != nil {
		fmt.Fprintf(w, "внимание: журнал событий не настроен: %v\n", err)
	}

	batPath, err := WriteBat(dataDir, exePath, configPath)
	if err != nil {
		return err
	}

	fmt.Fprintf(w, "служба %s установлена\n", serviceName)
	fmt.Fprintf(w, "исполняемый файл: %s\n", exePath)
	fmt.Fprintf(w, "конфиг:           %s\n", configPath)
	fmt.Fprintf(w, "хук transmission: %s\n\n", batPath)
	fmt.Fprintln(w, SettingsHint(batPath))
	fmt.Fprintf(w, "\nЗапуск службы: sc start %s\n", serviceName)
	return nil
}

// Uninstall удаляет службу. Каталог данных и конфиг остаются на месте.
func Uninstall(serviceName string, w io.Writer) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("подключение к диспетчеру служб (запустите от администратора): %w", err)
	}
	defer m.Disconnect()

	service, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("служба %s не найдена: %w", serviceName, err)
	}
	defer service.Close()

	if err := service.Delete(); err != nil {
		return fmt.Errorf("удаление службы %s: %w", serviceName, err)
	}
	if err := eventlog.Remove(serviceName); err != nil {
		fmt.Fprintf(w, "внимание: запись журнала событий не удалена: %v\n", err)
	}

	fmt.Fprintf(w, "служба %s удалена; конфиг, состояние и логи остались на месте\n", serviceName)
	return nil
}
