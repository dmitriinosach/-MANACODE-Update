package main

import (
	"debug/pe"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestProcessRunning(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if !processRunning(strings.ToUpper(filepath.Base(exe))) {
		t.Error("свой процесс не нашёлся")
	}
	if processRunning("нет-такой-игры.exe") {
		t.Error("несуществующий процесс найден")
	}
}

func TestSubprocessesHidden(t *testing.T) {
	c := hidden(exec.Command("schtasks"))
	if c.SysProcAttr == nil || !c.SysProcAttr.HideWindow || c.SysProcAttr.CreationFlags&createNoWindow == 0 {
		t.Errorf("дочерний процесс без CREATE_NO_WINDOW: %+v", c.SysProcAttr)
	}
	files, _ := filepath.Glob("*.go")
	bare := regexp.MustCompile(`(^|[^(])exec\.Command\(`)
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			if bare.MatchString(line) && !strings.Contains(line, "hidden(exec.Command(") {
				t.Errorf("%s:%d: запуск без hidden() — у фонового прогона мигнёт консоль: %s", f, i+1, strings.TrimSpace(line))
			}
		}
	}
}

func TestGUISubsystem(t *testing.T) {
	if testing.Short() {
		t.Skip("сборка exe — не в -short")
	}
	dir := t.TempDir()
	for name, tags := range map[string]string{"ManacodeUpdate.exe": ""} {
		out := filepath.Join(dir, name)
		cmd := exec.Command("go", "build", "-tags", tags, "-trimpath", "-ldflags=-s -w -H=windowsgui", "-o", out, ".")
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", name, err, b)
		}
		f, err := pe.Open(out)
		if err != nil {
			t.Fatal(err)
		}
		oh, ok := f.OptionalHeader.(*pe.OptionalHeader64)
		f.Close()
		if !ok || oh.Subsystem != pe.IMAGE_SUBSYSTEM_WINDOWS_GUI {
			t.Errorf("%s: подсистема не GUI — Windows откроет консоль при каждом запуске", name)
		}
	}
}
