package main

import (
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type gameProcess struct {
	pid     uint32
	started time.Time
}

type gameFilePolicy struct {
	mu        sync.Mutex
	processes func() ([]gameProcess, error)
	now       func() time.Time
	seen      map[uint32]bool
	lastExit  time.Time
	lastStart time.Time
}

var errGameFilesWait = localizedError("файлы игры заняты — повторите после выхода из игры")

var gameFileAccess = &gameFilePolicy{
	processes: wowProcesses,
	now:       time.Now,
	lastExit:  time.Now(),
}

func wowProcesses() ([]gameProcess, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snap)
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	var out []gameProcess
	for err = windows.Process32First(snap, &entry); err == nil; err = windows.Process32Next(snap, &entry) {
		if !strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), gameExe) {
			continue
		}
		started := time.Now()
		handle, openErr := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, entry.ProcessID)
		if openErr == nil {
			var creation, exit, kernel, user windows.Filetime
			if windows.GetProcessTimes(handle, &creation, &exit, &kernel, &user) == nil {
				started = time.Unix(0, creation.Nanoseconds())
			}
			windows.CloseHandle(handle)
		}
		out = append(out, gameProcess{pid: entry.ProcessID, started: started})
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return nil, err
	}
	return out, nil
}

func (p *gameFilePolicy) check(savedVariables bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	processes, err := p.processes()
	if err != nil {
		return errGameFilesWait
	}
	now, current := p.now(), map[uint32]bool{}
	for _, process := range processes {
		current[process.pid] = true
		if process.started.After(p.lastStart) {
			p.lastStart = process.started
		}
	}
	for pid := range p.seen {
		if !current[pid] {
			p.lastExit = now
		}
	}
	p.seen = current
	if savedVariables {
		if len(current) > 0 || now.Sub(p.lastExit) < time.Minute {
			return errGameFilesWait
		}
	} else if now.Sub(p.lastStart) < 2*time.Minute {
		return errGameFilesWait
	}
	return nil
}

func readSavedVariables(path string) ([]byte, error) {
	if err := gameFileAccess.check(true); err != nil {
		return nil, err
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(
		name, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0,
	)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(handle), path)
	data, err := io.ReadAll(file)
	closeErr := file.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if err := gameFileAccess.check(true); err != nil {
		return nil, err
	}
	return data, nil
}

func addonWriteReady() error {
	return gameFileAccess.check(false)
}

func renameAddon(src, dst string) error {
	if err := addonWriteReady(); err != nil {
		return err
	}
	return os.Rename(src, dst)
}

func rollbackRename(src, dst string) error {
	return os.Rename(src, dst)
}

func clearStale(dst, stale string) error {
	if _, err := os.Lstat(stale); err != nil {
		return nil
	}
	if _, err := os.Lstat(dst); os.IsNotExist(err) {
		return rollbackRename(stale, dst)
	}
	return os.RemoveAll(stale)
}
