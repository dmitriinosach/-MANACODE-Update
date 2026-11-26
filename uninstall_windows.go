package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"unsafe"
)

func (u *gui) confirmRemoval(key string) {
	plan, err := planAddonRemoval(u.app.view().addons, key)
	if err != nil {
		u.message(err.Error())
		return
	}
	names := make([]string, 0, len(plan.folders))
	for _, target := range plan.folders {
		names = append(names, filepath.Base(target))
	}
	question := fmt.Sprintf(
		tr("Удалить %s?\n\nПапки:\n%s\n\nНастройки персонажей в WTF останутся. Для повторной установки нажмите «Установить»."),
		plan.title, strings.Join(names, "\n"),
	)
	answer, _, _ := uMessageBox.Call(
		u.hwnd, uintptr(unsafe.Pointer(u16p(question))),
		uintptr(unsafe.Pointer(u16p(tr("Удаление аддона")))), 0x134,
	)
	if answer == 6 {
		u.app.removeAddon(key)
	}
}
