package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/windows/registry"
)

const shotTimeout = 60 * time.Second

var browserPath = findBrowser

func findBrowser() string {
	for _, exe := range []string{"msedge.exe", "chrome.exe"} {
		for _, root := range []registry.Key{registry.CURRENT_USER, registry.LOCAL_MACHINE} {
			k, err := registry.OpenKey(root, `SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\`+exe, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			p, _, err := k.GetStringValue("")
			k.Close()
			if p = strings.Trim(p, `"`); err == nil && isFile(p) {
				return p
			}
		}
		sub := `Microsoft\Edge\Application\msedge.exe`
		if exe == "chrome.exe" {
			sub = `Google\Chrome\Application\chrome.exe`
		}
		for _, env := range []string{"ProgramFiles(x86)", "ProgramFiles", "LOCALAPPDATA"} {
			if dir := os.Getenv(env); dir != "" {
				if p := filepath.Join(dir, sub); isFile(p) {
					return p
				}
			}
		}
	}
	return ""
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func fileURL(p string) string {
	u := url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(p)}
	return u.String()
}

func shootPages(exe string, pages []htmlPage) ([][]byte, error) {
	tmp, err := os.MkdirTemp("", "manacode-discord-")
	if err != nil {
		return nil, err
	}
	defer removeLater(tmp)
	out := make([][]byte, len(pages))
	errs := make([]error, len(pages))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 3)
	for i, pg := range pages {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out[i], errs[i] = shootPage(exe, filepath.Join(tmp, fmt.Sprint(i)), pg)
		}()
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return out, nil
}

func shootPage(exe, dir string, pg htmlPage) ([]byte, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	page := filepath.Join(dir, "page.html")
	shot := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(page, []byte(pg.HTML), 0o644); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), shotTimeout)
	defer cancel()
	cmd := hidden(exec.CommandContext(ctx, exe,
		"--headless=new", "--disable-gpu", "--hide-scrollbars", "--no-first-run", "--no-default-browser-check",
		"--disable-extensions", "--mute-audio",
		"--user-data-dir="+filepath.Join(dir, "profile"),
		fmt.Sprintf("--window-size=%d,%d", pg.W, pg.H),
		"--force-device-scale-factor=2",
		"--screenshot="+shot,
		fileURL(page)))
	runErr := cmd.Run()
	var pic []byte
	for wait := 0; wait < 20; wait++ {
		if b, err := os.ReadFile(shot); err == nil && len(b) > 0 {
			if cfg, err := png.DecodeConfig(bytes.NewReader(b)); err == nil {
				if cfg.Width != pg.W*2 || cfg.Height != pg.H*2 {
					return nil, fmt.Errorf("снимок %d×%d вместо %d×%d", cfg.Width, cfg.Height, pg.W*2, pg.H*2)
				}
				pic = b
				break
			}
		}
		if ctx.Err() != nil {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if pic == nil {
		if runErr != nil {
			return nil, fmt.Errorf("браузер не снял страницу: %w", runErr)
		}
		return nil, errors.New("браузер не снял страницу")
	}
	return pic, nil
}

func removeLater(dir string) {
	for try := 0; try < 10; try++ {
		if os.RemoveAll(dir) == nil {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
}

type discordPic struct {
	Name string
	Data []byte
}

func renderPics(addons string, p discordPost) ([]discordPic, error) {
	if exe := browserPath(); exe != "" {
		pages := postPages(p, loadArt(gameDir(addons), p))
		if shots, err := shootPages(exe, pages); err == nil {
			out := make([]discordPic, len(shots))
			for i, b := range shots {
				out[i] = discordPic{Name: picName(p, i+1, len(shots)), Data: b}
			}
			return out, nil
		}
	}
	pic, err := renderPost(p)
	if err != nil {
		return nil, err
	}
	return []discordPic{{Name: picName(p, 1, 1), Data: pic}}, nil
}
