package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type marketView struct {
	cfg      marketSettings
	accounts []string
	note     string
	noteErr  bool
	busy     bool
	sent     int
	total    int
	checking bool
	signedIn bool
	game     bool
	st       marketStatus
}

func ensureMarketSettings() (string, error) {
	p := marketPath()
	if _, err := os.Stat(p); os.IsNotExist(err) {
		if err := editMarket(func(*marketSettings) {}); err != nil {
			return "", err
		}
	}
	return p, nil
}

func (a *app) marketView() marketView {
	a.mu.Lock()
	defer a.mu.Unlock()
	cfg := a.marketConfigLocked()
	v := marketView{cfg: *cfg, note: a.marketNote, noteErr: a.marketNoteErr, busy: a.marketBusy, checking: a.marketChecking,
		sent: a.marketSent, total: a.marketTotal,
		signedIn: cfg.Token != "", game: a.game, st: *a.marketStatusLocked(), accounts: []string{}}
	v.cfg.Token = ""
	for _, path := range marketFiles(a.addons) {
		v.accounts = append(v.accounts, filepath.Base(filepath.Dir(filepath.Dir(path))))
	}
	return v
}

func (a *app) marketConfigLocked() *marketSettings {
	st, err := os.Stat(marketPath())
	if a.marketConfig == nil || (err == nil && !st.ModTime().Equal(a.marketConfigAt)) {
		cfg := loadMarket()
		a.marketConfig = &cfg
		if err == nil {
			a.marketConfigAt = st.ModTime()
		}
	}
	return a.marketConfig
}

func (a *app) marketStatusLocked() *marketStatus {
	st, err := os.Stat(marketStatusPath())
	if a.marketStatus == nil || (err == nil && !st.ModTime().Equal(a.marketStatusAt)) || (err != nil && !a.marketStatusAt.IsZero()) {
		status := loadMarketStatus()
		a.marketStatus = &status
		a.marketStatusAt = time.Time{}
		if err == nil {
			a.marketStatusAt = st.ModTime()
		}
	}
	return a.marketStatus
}

func (a *app) marketDays(days int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.marketBusy {
		return
	}
	if days != 7 && days != 30 && days != 90 && days != 0 {
		return
	}
	if loadMarket().Days == days {
		return
	}
	if err := editMarket(func(c *marketSettings) { c.Days = days }); err != nil {
		a.marketNote, a.marketNoteErr = tr(err.Error()), true
	}
	a.marketAt = time.Time{}
}

func (a *app) marketAccount(account string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.marketBusy {
		return
	}
	if loadMarket().Account == account {
		return
	}
	signedIn := false
	err := editMarket(func(c *marketSettings) {
		signedIn = c.Token != ""
		c.Token, c.Secret, c.Account = "", "", account
	})
	if signedIn {
		a.marketNote, a.marketNoteErr = tr("Аккаунт со сканами сменён — подключитесь к базе заново: у каждого аккаунта свой источник."), false
	}
	if err != nil {
		a.marketNote, a.marketNoteErr = tr(err.Error()), true
	}
}

func (a *app) loginMarket(login, password string) {
	a.mu.Lock()
	if a.marketBusy {
		a.mu.Unlock()
		return
	}
	a.marketBusy = true
	a.marketNote, a.marketNoteErr = "", false
	addons := a.addons
	a.mu.Unlock()
	a.changed()
	go func() {
		cfg := loadMarket()
		if cfg.Account == "" {
			cfg.Account = marketNewestAccount(addons)
		}
		err := marketLogin(context.Background(), &cfg, login, password)
		note, bad := tr("Доступ к базе получен — забираю историю рынка."), false
		if err != nil {
			note, bad = tr("Не подключено: ")+tr(err.Error()), true
		}
		a.mu.Lock()
		a.marketBusy, a.marketNote, a.marketNoteErr = false, note, bad
		a.mu.Unlock()
		a.changed()
		if err == nil {
			for a.installing() {
				time.Sleep(time.Second)
			}
			a.syncMarket(true, true)
		}
	}()
}

func (a *app) installing() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.busy != ""
}

func (a *app) logoutMarket() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.marketBusy {
		return
	}
	if err := editMarket(func(c *marketSettings) { c.Token, c.Secret = "", "" }); err != nil {
		a.marketNote, a.marketNoteErr = tr(err.Error()), true
		return
	}
	updateMarketStatus(func(st *marketStatus) { st.Link, st.LinkNote, st.LinkAt = "", "", 0 })
	a.marketNote, a.marketNoteErr = tr("Отключено от базы рынка. Сканы и скачанная история остаются на диске."), false
}

func dropMarketToken(token string) {
	if token == "" {
		return
	}
	_ = editMarket(func(c *marketSettings) {
		if c.Token == token {
			c.Token, c.Secret = "", ""
		}
	})
}

func (a *app) checkMarket() {
	a.mu.Lock()
	if a.marketBusy || a.marketChecking {
		a.mu.Unlock()
		return
	}
	a.marketChecking = true
	a.mu.Unlock()
	a.changed()
	go func() {
		cfg := loadMarket()
		err := marketCheck(context.Background(), cfg)
		if errors.Is(err, errMarketDenied) {
			dropMarketToken(cfg.Token)
		}
		a.mu.Lock()
		a.marketChecking = false
		switch {
		case errors.Is(err, errMarketDenied):
			a.marketNote, a.marketNoteErr = tr("Доступ к базе больше не действует — подключитесь заново."), true
		case err == nil && a.marketNoteErr:
			a.marketNote, a.marketNoteErr = "", false
		}
		a.mu.Unlock()
		a.changed()
	}()
}

func (a *app) syncMarket(push, manual bool) {
	a.mu.Lock()
	if a.marketBusy || a.busy != "" {
		a.mu.Unlock()
		return
	}
	a.marketBusy = true
	a.marketAt = time.Now()
	a.marketSent, a.marketTotal = 0, 0
	if manual || a.marketNoteErr {
		a.marketNote, a.marketNoteErr = "", false
	}
	addons := a.addons
	stamp := marketFileStamp(addons, loadMarket().Account)
	a.mu.Unlock()
	a.changed()
	go func() {
		cfg := loadMarket()
		res, err := marketSync(context.Background(), addons, &cfg, push, func(sent, total int) {
			a.mu.Lock()
			a.marketSent, a.marketTotal = sent, total
			a.mu.Unlock()
			a.changed()
		})
		if errors.Is(err, errMarketDenied) {
			dropMarketToken(cfg.Token)
		}
		switch {
		case errors.Is(err, errMarketRunning):
		case err != nil:
			autoLog("рынок: обмен не завершён: %v", err)
		case res.Sent > 0 || res.Added > 0 || res.Installed || res.Held != "":
			autoLog("рынок: %s", res.note())
		}
		a.mu.Lock()
		a.marketBusy = false
		switch {
		case errors.Is(err, errMarketRunning):
		case errors.Is(err, errMarketDenied):
			a.marketNote, a.marketNoteErr = tr("Доступ к базе больше не действует — подключитесь заново. ")+tr(err.Error()), true
		case err != nil:
			a.marketNote, a.marketNoteErr = fmt.Sprintf(tr("Обмен не завершён: %v"), err), true
		case manual:
			a.marketNote, a.marketNoteErr = res.note(), res.Held != ""
		}
		if err == nil && (res.Pushed || res.Held != "") {
			a.marketStamp = stamp
		}
		a.mu.Unlock()
		a.changed()
	}()
}

func marketFileStamp(addons, account string) string {
	for _, path := range marketFiles(addons) {
		if filepath.Base(filepath.Dir(filepath.Dir(path))) == account {
			if st, err := os.Stat(path); err == nil {
				return fmt.Sprintf("%s|%d|%d", path, st.Size(), st.ModTime().UnixNano())
			}
		}
	}
	return ""
}

func (a *app) marketPoll() {
	a.mu.Lock()
	cfg := *a.marketConfigLocked()
	st := *a.marketStatusLocked()
	if cfg.URL == "" || cfg.Token == "" || a.marketBusy || a.busy != "" {
		a.mu.Unlock()
		return
	}
	stamp := marketFileStamp(a.addons, cfg.Account)
	since := time.Since(a.marketAt)
	changed := stamp != "" && stamp != a.marketStamp
	install := st.Pending && !a.game && since > 30*time.Second
	due := install || (cfg.Automatic && (since > marketPullEvery || (changed && since > time.Minute)))
	a.mu.Unlock()
	if due {
		a.syncMarket(changed, false)
	}
}

func (a *app) marketActive() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.marketBusy || a.marketChecking
}

func (a *app) marketLinkPoll() {
	a.mu.Lock()
	cfg := *a.marketConfigLocked()
	st := *a.marketStatusLocked()
	due := cfg.Token != "" && !a.marketBusy && !a.marketChecking && time.Since(time.Unix(st.LinkAt, 0)) > marketLinkEvery
	a.mu.Unlock()
	if due {
		a.checkMarket()
	}
}

func (a *app) marketAutomatic() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.marketBusy {
		return
	}
	if err := editMarket(func(c *marketSettings) { c.Automatic = !c.Automatic }); err != nil {
		a.marketNote, a.marketNoteErr = tr(err.Error()), true
	}
	a.marketAt = time.Time{}
}
