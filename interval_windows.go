package main

import "unsafe"

var (
	uCreatePopupMenu = wUser32.NewProc("CreatePopupMenu")
	uAppendMenu      = wUser32.NewProc("AppendMenuW")
	uTrackPopupMenu  = wUser32.NewProc("TrackPopupMenuEx")
	uDestroyMenu     = wUser32.NewProc("DestroyMenu")
	uGetCursorPos    = wUser32.NewProc("GetCursorPos")
)

func (u *gui) pickHours() {
	menu, _, _ := uCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer uDestroyMenu.Call(menu)
	current := u.app.view().hours
	for _, hours := range addonIntervals {
		var flags uintptr
		if hours == current {
			flags = 0x8
		}
		uAppendMenu.Call(menu, flags, uintptr(hours), uintptr(unsafe.Pointer(u16p(hoursRU(hours)))))
	}
	var cursor winPoint
	uGetCursorPos.Call(uintptr(unsafe.Pointer(&cursor)))
	selected, _, _ := uTrackPopupMenu.Call(
		menu, 0x100|0x80, uintptr(cursor.x), uintptr(cursor.y), u.hwnd, 0,
	)
	if selected != 0 {
		u.app.selectHours(int(selected))
	}
}
