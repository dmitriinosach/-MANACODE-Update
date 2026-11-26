package main

import (
	"syscall"
	"testing"
	"unsafe"
)

func TestOpenCopyOlder(t *testing.T) {
	if guiTitle() != "ManacodeUpdate "+updaterVersion {
		t.Errorf("заголовок: %q", guiTitle())
	}
	for title, want := range map[string]bool{
		"Manacode — обновление аддонов": true,
		"ManacodeUpdate 1.1.9":          true,
		guiTitle():                      false,
		"ManacodeUpdate 9.0.0":          false,
	} {
		if _, older := openCopyOlder(title); older != want {
			t.Errorf("%q: старее %t, ждали %t", title, older, want)
		}
	}
}

func TestGUIMutexOnce(t *testing.T) {
	unlock, ok := lockWatch(guiMutex + "_test")
	if !ok {
		t.Fatal("первая копия не взяла замок")
	}
	if _, again := lockWatch(guiMutex + "_test"); again {
		t.Error("вторая копия тоже взяла замок")
	}
	unlock()
	if u2, ok := lockWatch(guiMutex + "_test"); !ok {
		t.Error("после закрытия первой копии замок свободен")
	} else {
		u2()
	}
}

func TestCopyText(t *testing.T) {
	data, _ := syscall.UTF16FromString(newerNotice())
	cds := copyData{kind: copyNewer, size: uint32(len(data) * 2), ptr: &data[0]}
	if s, ok := copyText(uintptr(unsafe.Pointer(&cds))); !ok || s != newerNotice() {
		t.Errorf("строка из второй копии: %q %t", s, ok)
	}
	cds.kind = 1
	if _, ok := copyText(uintptr(unsafe.Pointer(&cds))); ok {
		t.Error("чужое WM_COPYDATA принято")
	}
}
