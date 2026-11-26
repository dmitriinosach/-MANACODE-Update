package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const (
	laaFlag   = 0x0020
	peChars   = 22
	peSum     = 24 + 64
	laaBtn    = "Включить 4 ГБ памяти"
	laaIdle   = "Wow.exe видит только 2 ГБ памяти — после долгого рейда игра может вылететь с «not enough memory». Кнопка сохранит копию Wow.exe.bak и включит 4 ГБ (флаг Large Address Aware). Игра должна быть закрыта."
	laaHave   = "Wow.exe видит 4 ГБ памяти (флаг Large Address Aware стоит)."
	laaNoExe  = "Клиент игры не найден: рядом с Interface\\AddOns нет Wow.exe."
	laaNotWow = "Wow.exe не похож на клиент 3.3.5a (не 32-битный exe) — не трогали."
)

var errNotWow = errors.New(laaNotWow)

type peHead struct {
	at    int64
	chars uint16
	sum   uint32
	raw   []byte
}

func (h peHead) laa() bool { return h.chars&laaFlag != 0 }

func readPE(r io.ReaderAt) (peHead, error) {
	le := binary.LittleEndian
	dos := make([]byte, 0x40)
	if _, err := r.ReadAt(dos, 0); err != nil || string(dos[:2]) != "MZ" {
		return peHead{}, errNotWow
	}
	at := int64(le.Uint32(dos[0x3C:]))
	raw := make([]byte, 24+96)
	if at < 0x40 || at > 1<<20 {
		return peHead{}, errNotWow
	}
	if _, err := r.ReadAt(raw, at); err != nil {
		return peHead{}, errNotWow
	}
	if string(raw[:4]) != "PE\x00\x00" || le.Uint16(raw[4:]) != 0x14C || le.Uint16(raw[24:]) != 0x10B {
		return peHead{}, errNotWow
	}
	return peHead{at: at, chars: le.Uint16(raw[peChars:]), sum: le.Uint32(raw[peSum:]), raw: raw}, nil
}

func peChecksum(b []byte, sumAt int) uint32 {
	var s uint64
	n := len(b) &^ 1
	for i := 0; i < n; i += 2 {
		if i >= sumAt && i < sumAt+4 {
			continue
		}
		s += uint64(binary.LittleEndian.Uint16(b[i:]))
		s = (s & 0xFFFF) + (s >> 16)
	}
	if len(b)&1 != 0 {
		s += uint64(b[len(b)-1])
		s = (s & 0xFFFF) + (s >> 16)
	}
	s = (s & 0xFFFF) + (s >> 16)
	return uint32(s) + uint32(len(b))
}

func laaState(exe string) (bool, error) {
	f, err := os.Open(exe)
	if err != nil {
		return false, err
	}
	defer f.Close()
	h, err := readPE(f)
	if err != nil {
		return false, err
	}
	return h.laa(), nil
}

func laaBackup(exe string, b []byte) (string, error) {
	for i := 1; i <= 9; i++ {
		p := exe + ".bak"
		if i > 1 {
			p += fmt.Sprint(i)
		}
		old, err := os.ReadFile(p)
		if err == nil {
			if bytesSHA(old) == bytesSHA(b) {
				return p, nil
			}
			continue
		}
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return "", err
		}
		if _, err = f.Write(b); err == nil {
			err = f.Close()
		} else {
			f.Close()
		}
		if err != nil {
			_ = os.Remove(p)
			return "", err
		}
		return p, nil
	}
	return "", errors.New(tr("заняты имена Wow.exe.bak…bak9 — уберите лишние копии"))
}

func laaPatch(game string) (string, int, error) {
	exe := filepath.Join(game, gameExe)
	b, err := os.ReadFile(exe)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return laaNoExe, kindErr, nil
		}
		return "", kindErr, err
	}
	h, err := readPE(bytes.NewReader(b))
	if err != nil {
		return laaNotWow, kindErr, nil
	}
	if h.laa() {
		return laaHave, kindOK, nil
	}
	if gameRunning() {
		return lkRunning, kindErr, nil
	}
	bak, err := laaBackup(exe, b)
	if err != nil {
		return "", kindErr, fmt.Errorf(tr("не смог сохранить копию Wow.exe: %w"), err)
	}
	le := binary.LittleEndian
	head := append([]byte(nil), h.raw...)
	le.PutUint16(head[peChars:], h.chars|laaFlag)
	copy(b[h.at:], head)
	if h.sum != 0 {
		le.PutUint32(head[peSum:], peChecksum(b, int(h.at)+peSum))
	}
	f, err := os.OpenFile(exe, os.O_WRONLY, 0)
	if err != nil {
		return "", kindErr, fmt.Errorf(tr("не смог открыть Wow.exe на запись: %w"), err)
	}
	if _, err = f.WriteAt(head, h.at); err == nil {
		err = f.Close()
	} else {
		f.Close()
	}
	if err != nil {
		return "", kindErr, fmt.Errorf(tr("не смог записать Wow.exe (копия — %s): %w"), filepath.Base(bak), err)
	}
	if on, err := laaState(exe); err != nil || !on {
		return "", kindErr, fmt.Errorf(tr("флаг в Wow.exe не встал (копия — %s)"), filepath.Base(bak))
	}
	return fmt.Sprintf(tr("Включено: Wow.exe видит 4 ГБ памяти. Копия прежнего — %s."), filepath.Base(bak)), kindOK, nil
}

func laaRun(addons string) (string, int) {
	unlock, err := lockInstall(addons)
	if err != nil {
		return tr("Другая копия обновлялки сейчас ставит аддон или патч — нажмите ещё раз через минуту."), kindErr
	}
	defer unlock()
	text, kind, err := laaPatch(gameDir(addons))
	if err != nil {
		return tr("Не вышло: ") + err.Error(), kindErr
	}
	return text, kind
}

func laaCLI(addons string) int {
	text, kind := laaRun(addons)
	autoLog("4 ГБ памяти: %s", text)
	fmt.Println(text)
	if kind == kindErr {
		return 1
	}
	return 0
}

type laaSeen struct {
	path string
	mod  time.Time
	size int64
	on   bool
	err  error
}

func (a *app) laaFix() {
	a.mu.Lock()
	addons := a.addons
	if a.laaBusy || addons == "" {
		a.mu.Unlock()
		return
	}
	a.laaBusy, a.laaStatus, a.laaKind = true, tr("Смотрю Wow.exe…"), kindNone
	a.mu.Unlock()
	a.changed()
	go func() {
		text, kind := laaRun(addons)
		autoLog("4 ГБ памяти: %s", text)
		a.mu.Lock()
		a.laaBusy, a.laaStatus, a.laaKind = false, text, kind
		a.mu.Unlock()
		a.changed()
	}()
}

func (a *app) laaView() (busy, on bool, status string, kind int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	exe := ""
	if a.addons != "" {
		exe = filepath.Join(gameDir(a.addons), gameExe)
	}
	st, err := os.Stat(exe)
	s := &a.laaSeen
	if err != nil || exe == "" {
		*s = laaSeen{path: exe, err: err}
	} else if s.path != exe || !s.mod.Equal(st.ModTime()) || s.size != st.Size() || s.err != nil {
		on, err := laaState(exe)
		*s = laaSeen{path: exe, mod: st.ModTime(), size: st.Size(), on: on, err: err}
	}
	status, kind = a.laaStatus, a.laaKind
	if status == "" {
		switch {
		case s.err != nil && errors.Is(s.err, os.ErrNotExist) || exe == "":
			status, kind = laaNoExe, kindErr
		case s.err != nil:
			status, kind = laaNotWow, kindErr
		case s.on:
			status, kind = laaHave, kindOK
		default:
			status = laaIdle
		}
	}
	return a.laaBusy, s.err == nil && s.on, status, kind
}
