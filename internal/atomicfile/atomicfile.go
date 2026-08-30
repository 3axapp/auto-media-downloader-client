package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func WriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)

	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("создание временного файла в %s: %w", dir, err)
	}
	tmpName := tmp.Name()

	cleanup := func() {
		tmp.Close()
		os.Remove(tmpName)
	}

	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("запись во временный файл %s: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("сброс на диск %s: %w", tmpName, err)
	}
	if err := tmp.Chmod(perm); err != nil {
		cleanup()
		return fmt.Errorf("установка прав на %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("закрытие временного файла %s: %w", tmpName, err)
	}

	if err := renameWithRetry(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("переименование %s в %s: %w", tmpName, path, err)
	}
	return nil
}

func renameWithRetry(from, to string) error {
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if err = os.Rename(from, to); err == nil {
			return nil
		}
		time.Sleep(5 * time.Millisecond)
	}
	return err
}
