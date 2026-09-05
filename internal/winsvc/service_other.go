//go:build !windows

package winsvc

import (
	"context"
	"fmt"
)

func IsService() bool {
	return false
}
func Run(name string, work func(ctx context.Context) error) error {
	return fmt.Errorf("служба Windows недоступна на этой системе")
}
