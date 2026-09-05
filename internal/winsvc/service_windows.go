//go:build windows

package winsvc

import (
	"context"
	"fmt"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
)

const (
	eventStarted = 1
	eventStopped = 2
	eventFailed  = 3
)

func IsService() bool {
	isService, err := svc.IsWindowsService()
	if err != nil {
		return false
	}
	return isService
}

func Run(name string, work func(ctx context.Context) error) error {
	h := &handler{work: work}
	if elog, err := eventlog.Open(name); err == nil {
		h.elog = elog
		defer elog.Close()
	}

	if err := svc.Run(name, h); err != nil {
		if h.elog != nil {
			h.elog.Error(eventFailed, "не удалось запустить службу: "+err.Error())
		}
		return fmt.Errorf("работа службы %s: %w", name, err)
	}
	return nil
}

type handler struct {
	work func(ctx context.Context) error
	elog *eventlog.Log
}

func (h *handler) info(id uint32, msg string) {
	if h.elog != nil {
		h.elog.Info(id, msg)
	}
}

func (h *handler) fail(id uint32, msg string) {
	if h.elog != nil {
		h.elog.Error(id, msg)
	}
}

func (h *handler) Execute(args []string, reg <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown

	status <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- h.work(ctx)
	}()

	status <- svc.Status{State: svc.Running, Accepts: accepted}
	h.info(eventStarted, "служба amd-client запущена")

	for {
		select {
		case c := <-reg:
			switch c.Cmd {
			case svc.Interrogate:
				status <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				cancel()
				<-done
				h.info(eventStopped, "служба amd-client остановлена")
				return false, 0
			}
		case err := <-done:
			status <- svc.Status{State: svc.StopPending}
			if err != nil {
				h.fail(eventFailed, "служба amd-client завершилась с ошибкой: "+err.Error())
				return false, 1
			}
			h.info(eventStopped, "служба amd-client завершила работу")
			return false, 0
		}
	}
}
