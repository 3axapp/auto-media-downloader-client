package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestUsageListsAllCommands(t *testing.T) {
	var buf bytes.Buffer
	usage(&buf)

	for _, cmd := range []string{"run", "done", "check", "install", "uninstall", "version"} {
		if !strings.Contains(buf.String(), cmd) {
			t.Errorf("в справке нет команды %q:\n%s", cmd, buf.String())
		}
	}
}

func TestDispatchUnknownCommand(t *testing.T) {
	var buf bytes.Buffer
	code := dispatch([]string{"погулять"}, &buf)

	if code == 0 {
		t.Fatal("неизвестная команда должна давать ненулевой код возврата")
	}
	if !strings.Contains(buf.String(), "погулять") {
		t.Errorf("в сообщении нет имени команды:\n%s", buf.String())
	}
}

func TestDispatchWithoutArgsShowsUsage(t *testing.T) {
	var buf bytes.Buffer
	code := dispatch(nil, &buf)

	if code == 0 {
		t.Fatal("запуск без команды должен давать ненулевой код возврата")
	}
	if !strings.Contains(buf.String(), "amd-client") {
		t.Errorf("не показана справка:\n%s", buf.String())
	}
}

func TestDispatchVersion(t *testing.T) {
	var buf bytes.Buffer
	if code := dispatch([]string{"version"}, &buf); code != 0 {
		t.Fatalf("код возврата = %d", code)
	}
	if buf.Len() == 0 {
		t.Error("version ничего не вывела")
	}
}
