package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogRotates(t *testing.T) {
	stubUserConfig(t)
	path := filepath.Join(logsDir(), logFile)
	old := filepath.Join(logsDir(), "updater.1.log")
	put(t, path, strings.Repeat("a", logMax+1))
	autoLog("первая %d", 1)
	if st, err := os.Stat(old); err != nil || st.Size() != logMax+1 {
		t.Fatalf("большой журнал ушёл в updater.1.log: %v", err)
	}
	if got := read(path); !strings.Contains(got, "первая 1") || len(got) > 100 {
		t.Errorf("новый журнал: %q", got)
	}
	put(t, path, strings.Repeat("b", logMax+1))
	autoLog("вторая")
	entries, _ := os.ReadDir(logsDir())
	if len(entries) != 2 || !strings.HasPrefix(read(old), "b") {
		t.Errorf("держим два файла: %d, старый %q", len(entries), read(old)[:1])
	}
	dcLog("discord")
	if !strings.Contains(read(filepath.Join(logsDir(), discordLogFile)), "discord") {
		t.Error("discord.log рядом")
	}
}
