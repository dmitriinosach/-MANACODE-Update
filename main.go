package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func main() {
	args := os.Args[1:]
	arg := ""
	if len(args) > 0 {
		arg = args[0]
	}
	gui := arg == "" || arg == "-shot"
	if !gui && arg != "-auto" && arg != "-watch" {
		parentConsole()
	}
	exe, err := exePath()
	if err != nil {
		fmt.Println("ОШИБКА:", err)
		os.Exit(1)
	}
	cleanOldExe()
	addons := findAddOns(filepath.Dir(exe))
	setupHome(addons)
	if arg != "-shot" {
		tidyAddons(addons)
	}
	if gui {
		shot, key, test := "", "", false
		if arg == "-shot" && len(args) > 1 {
			shot = args[1]
			for _, a := range args[2:] {
				if a == "test" {
					test, key = true, "rh"
				} else {
					key = a
				}
			}
		}
		os.Exit(runGUI(addons, shot, key, test))
	}
	if arg == "-version" {
		fmt.Println(ownVersion())
		return
	}
	if addons == "" {
		fmt.Println("ОШИБКА: не нашёл папку игры — положите " + updaterExe + " в папку аддона в Interface\\AddOns или запустите без ключей и укажите её")
		os.Exit(1)
	}
	cnt := &counter{}
	dir := priDir(addons)
	switch arg {
	case "-auto":
		os.Exit(runAuto(addons, cnt))
	case "-watch":
		os.Exit(runWatch(addons, cnt))
	case "-autostart":
		on := ""
		if len(args) > 1 {
			on = args[1]
		}
		os.Exit(autostartCLI(dir, on))
	case "-addon", "-addon-zip":
		os.Exit(addonCLI(dir, cnt))
	case "-from":
		if len(args) < 2 {
			fmt.Println("ОШИБКА: -from N")
			os.Exit(1)
		}
		from, err := strconv.Atoi(args[1])
		if err != nil {
			fmt.Println("ОШИБКА: -from N")
			os.Exit(1)
		}
		if localFormat(dir) >= formatV13 {
			os.Exit(from13(dir, from, cnt))
		}
		os.Exit(fromCLI(dir, from, cnt))
	}
	fmt.Println("ключи: -auto | -watch | -autostart on|off | -addon | -addon-zip файл.zip | -from N | -version | -shot файл.png [pri|rh|arc|buff|vendor|test]; без ключей — окно")
	os.Exit(1)
}

func fromCLI(dir string, from int, cnt *counter) int {
	idx, err := fetchIndex()
	if err != nil {
		fmt.Println("ОШИБКА:", err)
		return 1
	}
	if err := formatMismatch(dir, idx); err != nil {
		fmt.Println("ОШИБКА:", err)
		return 1
	}
	sel := map[int]bool{}
	for _, s := range seasonsOf(*idx) {
		if s >= from {
			sel[s] = true
		}
	}
	written, err := download(*idx, dir, sel, cnt)
	if err != nil {
		fmt.Println("ОШИБКА:", err)
		return 1
	}
	a := readLocal(dir)
	fmt.Printf("записано: %s\nвыгрузка %s, игроков %d, полная %t, сезоны %v\n", strings.Join(written, ", "), a.Baked, a.Count, a.Complete, a.Seasons)
	if gm, err := syncGM(dir, idx.GM, cnt); err != nil {
		fmt.Println("ГМ:", err)
	} else if gm != "" {
		fmt.Println("ГМ:", gm)
	}
	return 0
}
