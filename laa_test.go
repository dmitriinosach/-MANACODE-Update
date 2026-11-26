package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func fakePE(chars uint16, machine uint16, withSum bool) []byte {
	b := make([]byte, 1501)
	le := binary.LittleEndian
	copy(b, "MZ")
	le.PutUint32(b[0x3C:], 0x80)
	copy(b[0x80:], "PE\x00\x00")
	le.PutUint16(b[0x84:], machine)
	le.PutUint16(b[0x80+peChars:], chars)
	le.PutUint16(b[0x80+24:], 0x10B)
	for i := 0x200; i < len(b); i++ {
		b[i] = byte(i * 7)
	}
	if withSum {
		le.PutUint32(b[0x80+peSum:], peChecksum(b, 0x80+peSum))
	}
	return b
}

func peSumOf(t *testing.T, b []byte) (stored, want uint32) {
	t.Helper()
	h, err := readPE(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return h.sum, peChecksum(b, int(h.at)+peSum)
}

func laaGame(t *testing.T, exe []byte) (string, string) {
	t.Helper()
	game := t.TempDir()
	p := filepath.Join(game, gameExe)
	if exe != nil {
		if err := os.WriteFile(p, exe, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return game, p
}

func diffBytes(a, b []byte) int {
	n := 0
	for i := range a {
		if a[i] != b[i] {
			n++
		}
	}
	return n
}

func TestLAAPatch(t *testing.T) {
	stubGame(t, false)
	orig := fakePE(0x0102, 0x14C, true)
	game, exe := laaGame(t, orig)
	if on, err := laaState(exe); err != nil || on {
		t.Fatalf("до: on=%v err=%v", on, err)
	}
	stubGame(t, true)
	text, kind, err := laaPatch(game)
	if err != nil || kind != kindErr || text != lkRunning {
		t.Fatalf("игра запущена: %q %d %v", text, kind, err)
	}
	if now, _ := os.ReadFile(exe); !bytes.Equal(now, orig) {
		t.Fatal("игра запущена, а Wow.exe изменён")
	}
	if _, err := os.Stat(exe + ".bak"); err == nil {
		t.Fatal("игра запущена, а копия сделана")
	}
	stubGame(t, false)
	text, kind, err = laaPatch(game)
	if err != nil || kind != kindOK {
		t.Fatalf("поставить: %q %d %v", text, kind, err)
	}
	now, _ := os.ReadFile(exe)
	if on, _ := laaState(exe); !on {
		t.Fatal("после: флага нет")
	}
	if s, w := peSumOf(t, now); s != w {
		t.Fatalf("контрольная сумма %x, нужна %x", s, w)
	}
	if n := diffBytes(orig, now); n < 1 || n > 5 {
		t.Fatalf("изменено байт: %d", n)
	}
	if bak, _ := os.ReadFile(exe + ".bak"); !bytes.Equal(bak, orig) {
		t.Fatal("копия не равна прежнему Wow.exe")
	}
	text, kind, _ = laaPatch(game)
	if kind != kindOK || text != laaHave {
		t.Fatalf("второй раз: %q", text)
	}
	if _, err := os.Stat(exe + ".bak2"); err == nil {
		t.Fatal("второй раз — лишняя копия")
	}
}

func TestLAABackupNames(t *testing.T) {
	stubGame(t, false)
	orig := fakePE(0x0102, 0x14C, false)
	game, exe := laaGame(t, orig)
	put(t, exe+".bak", "чужая копия")
	if _, kind, err := laaPatch(game); err != nil || kind != kindOK {
		t.Fatal(kind, err)
	}
	if b, _ := os.ReadFile(exe + ".bak"); string(b) != "чужая копия" {
		t.Fatal("чужая копия перезаписана")
	}
	if b, _ := os.ReadFile(exe + ".bak2"); !bytes.Equal(b, orig) {
		t.Fatal("копии нет в Wow.exe.bak2")
	}
	now, _ := os.ReadFile(exe)
	if s, _ := peSumOf(t, now); s != 0 {
		t.Fatalf("нулевая сумма стала %x", s)
	}

	game, exe = laaGame(t, orig)
	put(t, exe+".bak", string(orig))
	if _, kind, err := laaPatch(game); err != nil || kind != kindOK {
		t.Fatal(kind, err)
	}
	if _, err := os.Stat(exe + ".bak2"); err == nil {
		t.Fatal("такая же копия уже была — вторая не нужна")
	}
}

func TestLAARefuses(t *testing.T) {
	stubGame(t, false)
	game, _ := laaGame(t, nil)
	if text, _, _ := laaPatch(game); text != laaNoExe {
		t.Fatalf("нет exe: %q", text)
	}
	for name, b := range map[string][]byte{
		"мусор":  []byte("не exe вовсе"),
		"64 бит": fakePE(0x0022, 0x8664, true),
	} {
		game, exe := laaGame(t, b)
		if text, kind, _ := laaPatch(game); text != laaNotWow || kind != kindErr {
			t.Fatalf("%s: %q", name, text)
		}
		if _, err := os.Stat(exe + ".bak"); err == nil {
			t.Fatalf("%s: копия сделана", name)
		}
	}
}

func TestPEChecksumWindows(t *testing.T) {
	dir := os.Getenv("SystemRoot")
	n := 0
	for _, name := range []string{"notepad.exe", "cmd.exe", "kernel32.dll", "user32.dll"} {
		b, err := os.ReadFile(filepath.Join(dir, "SysWOW64", name))
		if err != nil {
			continue
		}
		if s, w := peSumOf(t, b); s != w {
			t.Fatalf("%s: сумма %x, счёт даёт %x", name, s, w)
		}
		n++
	}
	if n == 0 {
		t.Skip("нет SysWOW64")
	}
}

func TestLAACustomerClient(t *testing.T) {
	src := os.Getenv("LAA_CLIENT")
	if src == "" {
		t.Skip("LAA_CLIENT не задан")
	}
	b, err := os.ReadFile(filepath.Join(src, gameExe))
	if err != nil {
		t.Fatal(err)
	}
	real := append([]byte(nil), b...)
	h, _ := readPE(bytes.NewReader(b))
	off := int(h.at)
	le := binary.LittleEndian
	_, withFlag := peSumOf(t, b)
	le.PutUint16(b[off+peChars:], h.chars&^laaFlag)
	noFlag := peChecksum(b, off+peSum)
	switch h.sum {
	case 0, withFlag:
	case noFlag:
		t.Logf("сумма в Wow.exe клиента %x посчитана до флага LAA — флаг ставили без пересчёта", h.sum)
	default:
		t.Fatalf("сумма в Wow.exe клиента %x, счёт даёт %x (с флагом) и %x (без)", h.sum, withFlag, noFlag)
	}
	if h.sum != 0 {
		le.PutUint32(b[off+peSum:], noFlag)
	}
	stubGame(t, false)
	game, exe := laaGame(t, b)
	if on, _ := laaState(exe); on {
		t.Fatal("копия без флага, а флаг есть")
	}
	text, kind, err := laaPatch(game)
	if err != nil || kind != kindOK {
		t.Fatalf("%q %v", text, err)
	}
	t.Log(text)
	now, _ := os.ReadFile(exe)
	if on, _ := laaState(exe); !on {
		t.Fatal("флаг не встал")
	}
	if s, w := peSumOf(t, now); s != w && s != 0 {
		t.Fatalf("сумма %x, нужна %x", s, w)
	}
	if bak, _ := os.ReadFile(exe + ".bak"); !bytes.Equal(bak, b) {
		t.Fatal("копия не та")
	}
	copy(real[off+peSum:off+peSum+4], now[off+peSum:off+peSum+4])
	if h.chars&laaFlag != 0 && !bytes.Equal(now, real) {
		t.Fatalf("после патча копия не равна Wow.exe клиента: разных байт %d", diffBytes(now, real))
	}
	t.Logf("Characteristics %04x → %04x, сумма %x", h.chars&^laaFlag, le.Uint16(now[off+peChars:]), le.Uint32(now[off+peSum:]))
}
