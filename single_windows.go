package main

import (
	"regexp"
	"syscall"
	"unsafe"
)

const (
	guiMutex   = "Local\\ManacodeUpdate"
	guiClass   = "ManacodeUpdate"
	wmCopyData = 0x004A
	copyNewer  = 0x4D43
)

var (
	uFindWindow     = wUser32.NewProc("FindWindowW")
	uGetWindowText  = wUser32.NewProc("GetWindowTextW")
	uIsIconic       = wUser32.NewProc("IsIconic")
	uSendMsgTimeout = wUser32.NewProc("SendMessageTimeoutW")
	reTitleVersion  = regexp.MustCompile(`(\d+\.\d+\.\d+)`)
	guiUnlock       = func() {}
)

type copyData struct {
	kind uintptr
	size uint32
	ptr  *uint16
}

func guiTitle() string {
	return "ManacodeUpdate " + ownVersion()
}

func openCopyOlder(title string) (string, bool) {
	m := reTitleVersion.FindStringSubmatch(title)
	if m == nil {
		return "", true
	}
	return m[1], newer(ownVersion(), m[1])
}

func newerNotice() string {
	return "Запущена новая версия " + ownVersion() + " — закройте эту и откройте новую"
}

func takeGUI() bool {
	unlock, ok := lockWatch(guiMutex)
	if ok {
		guiUnlock = unlock
		return true
	}
	raiseOpen()
	return false
}

func raiseOpen() {
	h, _, _ := uFindWindow.Call(uintptr(unsafe.Pointer(u16p(guiClass))), 0)
	if h == 0 {
		autoLog("второй запуск %s: окно уже открыто, но не нашлось", ownVersion())
		return
	}
	buf := make([]uint16, 256)
	n, _, _ := uGetWindowText.Call(h, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	other, older := openCopyOlder(syscall.UTF16ToString(buf[:n]))
	if older {
		text := newerNotice()
		autoLog("второй запуск %s: открыта копия %s — %s", ownVersion(), yesNo(other != "", other, "старее 1.2.1"), text)
		data, err := syscall.UTF16FromString(text)
		if err == nil {
			cds := copyData{kind: copyNewer, size: uint32(len(data) * 2), ptr: &data[0]}
			uSendMsgTimeout.Call(h, wmCopyData, 0, uintptr(unsafe.Pointer(&cds)), 0x2, 2000, 0)
		}
	} else {
		autoLog("второй запуск %s: окно уже открыто — вывожу его вперёд", ownVersion())
	}
	if r, _, _ := uIsIconic.Call(h); r != 0 {
		uShowWindow.Call(h, 9)
	}
	uSetForeground.Call(h)
}

func copyText(lParam uintptr) (string, bool) {
	cds := *(**copyData)(unsafe.Pointer(&lParam))
	if cds == nil || cds.kind != copyNewer || cds.ptr == nil || cds.size == 0 || cds.size > 4096 {
		return "", false
	}
	return syscall.UTF16ToString(unsafe.Slice(cds.ptr, cds.size/2)), true
}
