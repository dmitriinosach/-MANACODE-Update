package main

import (
	"syscall"
	"unsafe"
)

var moveLanguageFile = syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW")

func replaceLanguageFile(source, target string) error {
	s, err := syscall.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	d, err := syscall.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	r, _, callErr := moveLanguageFile.Call(uintptr(unsafe.Pointer(s)), uintptr(unsafe.Pointer(d)), 0x1|0x8)
	if r == 0 {
		return callErr
	}
	return nil
}

func (u *gui) addLanguageLink(bounds box) {
	u.add(bounds, "language", !u.app.view().busy)
}

func (u *gui) drawLanguage() {
	u.fill(box{x: 0, y: 0, w: u.w, h: u.h}, cBg)
	w := min(u.s(620), u.w-u.s(64))
	x, y := (u.w-w)/2, max(u.s(32), (u.h-u.s(360))/2)
	u.line(box{x: x, y: y, w: w, h: u.s(42)}, "ManacodeUpdate", "title", cGold)
	y += u.s(60)
	y += u.wrap(x, y, w, "Выберите язык / Choose your language / Elige tu idioma", "head", cBright) + u.s(16)
	y += u.wrap(
		x, y, w,
		"Выбор сохранится. Сменить язык: RU / EN / ES.\nYour choice will be saved. Change language: RU / EN / ES.\nSe guardará tu elección. Cambiar idioma: RU / EN / ES.",
		"body", cText,
	) + u.s(28)
	bw := (w - u.s(32)) / 3
	u.button(
		box{x: x, y: y, w: bw, h: u.s(48)},
		"Русский (R)", "lang:ru", true, activeLanguage.Load() == languageRU,
	)
	u.button(
		box{x: x + bw + u.s(16), y: y, w: bw, h: u.s(48)},
		"English (E)", "lang:en", true, activeLanguage.Load() == languageEN,
	)
	u.button(
		box{x: x + 2*(bw+u.s(16)), y: y, w: bw, h: u.s(48)},
		"Español (S)", "lang:es", true, activeLanguage.Load() == languageES,
	)
}

func (u *gui) chooseLanguage(language string) {
	if err := saveLanguage(language); err != nil {
		u.message("Cannot save language / Не удалось сохранить язык:\n" + err.Error())
		return
	}
	u.languagePrompt = false
	u.scroll = 0
	u.app.clearLocalizedNotes()
	u.app.refresh()
	u.invalidate()
}

func (a *app) clearLocalizedNotes() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, c := range a.cards {
		c.note = ""
		c.err, c.customErr, c.idxErr, c.gmErr = nil, nil, nil, nil
	}
	a.dcNote, a.marketNote, a.lkStatus, a.laaStatus = "", "", "", ""
	a.dcKind, a.lkKind, a.laaKind = kindNone, kindNone, kindNone
	a.marketNoteErr = false
	a.notice = ""
	a.autoErr, a.selfErr, a.selfFail = nil, nil, ""
	if a.customDraft != nil {
		a.customDraft.Err = ""
	}
	if a.test != nil {
		a.test.done, a.test.fail = "", nil
	}
}
