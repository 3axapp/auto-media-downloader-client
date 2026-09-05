//go:build !windows

package install

import (
	"fmt"
	"io"
)

func Install(serviceName, configPath string, w io.Writer) error {
	return fmt.Errorf("установка службы доступна только в Windows")
}

func Uninstall(serviceName string, w io.Writer) error {
	return fmt.Errorf("удаление службы доступно только в Windows")
}
