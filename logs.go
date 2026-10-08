package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	logsSub        = "logs"
	logFile        = "updater.log"
	discordLogFile = "discord.log"
	logMax         = 1 << 20
)

var logMu sync.Mutex

func logsDir() string {
	h := appHome()
	if h == "" {
		return ""
	}
	return filepath.Join(h, logsSub)
}

func writeLog(name, format string, args ...any) {
	dir := logsDir()
	if dir == "" {
		return
	}
	logMu.Lock()
	defer logMu.Unlock()
	_ = os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, name)
	if st, err := os.Stat(path); err == nil && st.Size() > logMax {
		ext := filepath.Ext(name)
		old := filepath.Join(dir, strings.TrimSuffix(name, ext)+".1"+ext)
		_ = os.Remove(old)
		_ = os.Rename(path, old)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s  %s\n", now().Format("02.01.2006 15:04:05"), fmt.Sprintf(format, args...))
}

func autoLog(format string, args ...any) {
	writeLog(logFile, format, args...)
}

func dcLog(format string, args ...any) {
	writeLog(discordLogFile, format, args...)
}

func yesNo(ok bool, yes, no string) string {
	if ok {
		return yes
	}
	return no
}
