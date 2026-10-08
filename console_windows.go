package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procAllocConsole = kernel32.NewProc("AllocConsole")
	procAttach       = kernel32.NewProc("AttachConsole")
	procConsoleWnd   = kernel32.NewProc("GetConsoleWindow")
	procCreateMutex  = kernel32.NewProc("CreateMutexW")
)

const (
	attachParent  = ^uint32(0)
	errAlreadyRun = 183
	watchMutex    = "Local\\Manacode_Update"
)

func hasConsole() bool {
	w, _, _ := procConsoleWnd.Call()
	return w != 0
}

func bindConsole() {
	if f, err := os.OpenFile("CONOUT$", os.O_RDWR, 0); err == nil {
		os.Stdout, os.Stderr = f, f
	}
	if f, err := os.OpenFile("CONIN$", os.O_RDWR, 0); err == nil {
		os.Stdin = f
	}
}

func openConsole() {
	if hasConsole() {
		return
	}
	procAllocConsole.Call()
	bindConsole()
}

func parentConsole() {
	if hasConsole() {
		return
	}
	if _, err := os.Stdout.Stat(); err == nil {
		return
	}
	if r, _, _ := procAttach.Call(uintptr(attachParent)); r != 0 {
		bindConsole()
	}
}

func lockWatch(mutex string) (func(), bool) {
	name, err := syscall.UTF16PtrFromString(mutex)
	if err != nil {
		return func() {}, true
	}
	h, _, callErr := procCreateMutex.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		return func() {}, true
	}
	if errno, ok := callErr.(syscall.Errno); ok && errno == errAlreadyRun {
		syscall.CloseHandle(syscall.Handle(h))
		return func() {}, false
	}
	return func() { syscall.CloseHandle(syscall.Handle(h)) }, true
}

const createNoWindow = 0x08000000

func hidden(cmd *exec.Cmd) *exec.Cmd {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	return cmd
}

func explorerCmd(dir string) *exec.Cmd {
	cmd := hidden(exec.Command("explorer", dir))
	cmd.SysProcAttr.HideWindow = false
	return cmd
}

func openLogs() {
	dir := logsDir()
	if dir == "" {
		return
	}
	_ = os.MkdirAll(dir, 0o755)
	cmd := explorerCmd(dir)
	if cmd.Start() == nil {
		go cmd.Wait()
	}
}

func processRunning(name string) bool {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if strings.EqualFold(windows.UTF16ToString(e.ExeFile[:]), name) {
			return true
		}
	}
	return false
}

func stopLegacy(addons string) {
	if addons == "" {
		return
	}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return
	}
	defer windows.CloseHandle(snap)
	roots := legacyRoots(addons)
	self := uint32(os.Getpid())
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if e.ProcessID == self || !isLegacyName(windows.UTF16ToString(e.ExeFile[:])) {
			continue
		}
		h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_TERMINATE, false, e.ProcessID)
		if err != nil {
			continue
		}
		buf := make([]uint16, windows.MAX_LONG_PATH)
		n := uint32(len(buf))
		if windows.QueryFullProcessImageName(h, 0, &buf[0], &n) == nil {
			if underAny(windows.UTF16ToString(buf[:n]), roots) {
				_ = windows.TerminateProcess(h, 0)
			}
		}
		windows.CloseHandle(h)
	}
}

func legacyRoots(addons string) []string {
	var out []string
	add := func(p string) {
		if p != "" {
			out = append(out, strings.ToLower(filepath.Clean(p))+string(filepath.Separator))
		}
	}
	add(addons)
	add(realPath(addons))
	for _, d := range catalog {
		f := folderIn(addons, d.name)
		add(f)
		add(realPath(f))
	}
	return out
}

func underAny(path string, roots []string) bool {
	p := strings.ToLower(filepath.Clean(path))
	for _, r := range roots {
		if strings.HasPrefix(p, r) {
			return true
		}
	}
	return false
}
