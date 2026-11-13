package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const customNews = "custom-github-v1"

type customDraft struct {
	Repo     string
	Release  *customRelease
	Selected int
	Loading  bool
	Err      string
}

func newCustomCard(p customProject) *card {
	return &card{
		def:    addonDef{key: p.key(), title: p.title(), desc: p.Repo, repo: &p.Repo, kind: kindPlain},
		custom: &p,
	}
}

func (a *app) openCustom() {
	a.mu.Lock()
	if a.busy == "" {
		a.page = pageCustom
		if a.customDraft == nil {
			a.customDraft = &customDraft{}
		}
	}
	a.mu.Unlock()
	a.changed()
}

func (a *app) findCustom(input string) {
	a.mu.Lock()
	if a.busy != "" || a.addons == "" || a.page != pageCustom {
		a.mu.Unlock()
		return
	}
	d := &customDraft{Loading: true}
	a.customDraft = d
	a.busy = "custom-search"
	a.mu.Unlock()
	a.changed()
	go func() {
		repo, err := githubRepo(input)
		var release *customRelease
		if err == nil {
			release, err = findCustomRelease(repo)
		}
		a.mu.Lock()
		d.Repo, d.Release, d.Loading = repo, release, false
		if saved, exists := customByKey(loadConfig(), (customProject{Repo: repo}).key()); exists && release != nil {
			for i, asset := range release.Assets {
				if asset.Name == saved.Asset || asset.Pattern == saved.Asset {
					d.Selected = i
				}
			}
		}
		if err != nil {
			d.Err = tr(err.Error())
		}
		a.busy = ""
		a.mu.Unlock()
		a.changed()
	}()
}

func (a *app) customDraftView() customDraft {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.customDraft == nil {
		return customDraft{}
	}
	return *a.customDraft
}

func (a *app) clearCustomDraft() {
	a.mu.Lock()
	if a.busy == "" {
		a.customDraft = &customDraft{}
	}
	a.mu.Unlock()
}

func (a *app) selectCustomAsset(i int) {
	a.mu.Lock()
	d := a.customDraft
	if d != nil && d.Release != nil && i >= 0 && i < len(d.Release.Assets) {
		d.Selected = i
	}
	a.mu.Unlock()
	a.changed()
}

func (a *app) addCustom() {
	a.mu.Lock()
	defer a.changed()
	defer a.mu.Unlock()
	d := a.customDraft
	if d == nil || d.Release == nil || a.busy != "" {
		return
	}
	if d.Selected < 0 || d.Selected >= len(d.Release.Assets) {
		return
	}
	p := customProject{Repo: d.Repo, Asset: d.Release.Assets[d.Selected].Name}
	if pattern := d.Release.Assets[d.Selected].Pattern; pattern != "" {
		p.Asset = pattern
	}
	cfg := loadConfig()
	saved, exists := customByKey(cfg, p.key())
	for _, native := range catalog {
		if strings.EqualFold(*native.repo, p.Repo) {
			d.Err = tr("Этот проект уже есть во встроенном каталоге")
			return
		}
	}
	if exists {
		p.Installs = saved.Installs
		for i := range cfg.Custom {
			if cfg.Custom[i].key() == p.key() {
				cfg.Custom[i] = p
			}
		}
	} else {
		cfg.Custom = append(cfg.Custom, p)
	}
	if err := writeCustomConfig(cfg); err != nil {
		d.Err = tr("Не удалось сохранить проект: ") + err.Error()
		return
	}
	a.cfg = cfg
	c := a.cardOf(p.key())
	if c == nil {
		c = newCustomCard(p)
		a.cards = append(a.cards, c)
	}
	c.custom, c.customRel, c.customErr, c.err = &p, d.Release, nil, nil
	for i, candidate := range a.cards {
		if candidate == c {
			a.cur = i
		}
	}
	a.page = pageMain
	a.customDraft = nil
}

func (a *app) removeCustom(key string) {
	a.mu.Lock()
	defer a.changed()
	defer a.mu.Unlock()
	if a.busy != "" {
		return
	}
	c := a.cardOf(key)
	if c == nil || c.custom == nil {
		return
	}
	cfg := loadConfig()
	keep := []customProject{}
	for _, p := range cfg.Custom {
		if p.key() != key {
			keep = append(keep, p)
		}
	}
	cfg.Custom = keep
	if cfg.Auto != nil {
		cfg.Auto.drop(key)
	}
	if err := writeCustomConfig(cfg); err != nil {
		c.err = err
		return
	}
	a.cfg = cfg
	for i, card := range a.cards {
		if card == c {
			a.cards = append(a.cards[:i], a.cards[i+1:]...)
			break
		}
	}
	a.cur = min(a.cur, len(a.cards)-1)
}

func customInstalled(addons string, p customProject) bool {
	st := p.installed(addons)
	if st.Tag == "" || len(st.Folders) == 0 {
		return false
	}
	for _, folder := range st.Folders {
		target, err := customTarget(addons, folder)
		if err != nil || !isDir(target) {
			return false
		}
		if _, err := os.Stat(filepath.Join(target, folder+".toc")); err != nil {
			return false
		}
	}
	return true
}

func (a *app) checkCustom(c *card, addons string) {
	a.mu.Lock()
	p := *c.custom
	a.mu.Unlock()
	rel, err := findCustomRelease(p.Repo)
	if err == nil {
		_, err = rel.asset(p.Asset)
	}
	a.mu.Lock()
	if a.addons == addons && c.custom.Asset == p.Asset {
		if saved, ok := customByKey(loadConfig(), p.key()); ok {
			c.custom = &saved
		}
		c.customRel, c.customErr = rel, err
	}
	c.loading = false
	a.mu.Unlock()
	a.changed()
}

func (a *app) customAction(c *card) {
	if c.loading {
		return
	}
	p, rel := *c.custom, c.customRel
	installed := customInstalled(a.addons, p)
	current := installed && rel != nil && p.current(a.addons, rel.Tag)
	if rel == nil || c.customErr != nil || current {
		a.mu.Unlock()
		a.refresh()
		a.mu.Lock()
		return
	}
	addons, cnt := a.addons, a.cnt
	a.run(p.key(), p.title()+" "+rel.Tag, func() (string, error) {
		ver, err := installCustom(addons, p, rel, cnt)
		if err != nil {
			return "", err
		}
		return tr("Поставлен ") + ver + tr(" — перезапустите игру полностью"), nil
	}, func(c *card, note string, err error) {
		if err == nil {
			a.cfg = loadConfig()
			if saved, ok := customByKey(a.cfg, p.key()); ok {
				c.custom = &saved
			}
		}
	})
}

func (a *app) customCardView(c *card) cardView {
	p := *c.custom
	idle := a.busy == "" && a.addons != "" && !c.loading
	st := p.installed(a.addons)
	v := cardView{
		key: p.key(), title: p.title(), desc: tr("Свой проект GitHub · ") + p.Repo,
		page: releasesPage(p.Repo), custom: true, action: tr("Проверить"), actionOn: idle,
		ver: tr("не установлен"), short: tr("не установлен"),
	}
	installed := a.addons != "" && customInstalled(a.addons, p)
	if installed {
		v.ver, v.short = st.Tag, st.Tag
		v.folder, _ = customTarget(a.addons, st.Folders[0])
	}
	switch {
	case a.addons == "":
		v.verSub, v.short = tr("сначала укажите папку игры"), tr("папка игры не найдена")
	case c.loading:
		v.verSub, v.short = tr("спрашиваю GitHub…"), tr("проверяю…")
	case c.customErr != nil:
		v.verSub, v.short, v.dot = tr("не удалось проверить релиз"), tr("не удалось проверить"), kindErr
	case c.customRel == nil:
		v.verSub = tr("нажмите «Проверить»")
	case !installed:
		v.verSub = tr("последний стабильный релиз ") + c.customRel.Tag
		v.action, v.primary = tr("Установить"), true
	case st.Tag == c.customRel.Tag && st.Asset != p.Asset:
		v.verSub, v.short = tr("выбран другой архив"), tr("сменить архив")
		v.action, v.primary = tr("Установить"), true
	case !p.current(a.addons, c.customRel.Tag):
		v.verSub, v.short, v.dot = tr("вышла ")+c.customRel.Tag, tr("вышла ")+c.customRel.Tag, kindNew
		v.action, v.primary = tr("Обновить"), true
	default:
		v.verSub, v.dot, v.verKind = tr("последний стабильный релиз"), kindOK, kindOK
	}
	if c.err != nil {
		v.note, v.noteKind = tr(c.err.Error()), kindErr
	} else if c.customErr != nil {
		v.note, v.noteKind = tr(c.customErr.Error()), kindErr
	} else if c.note != "" {
		v.note, v.noteKind = c.note, kindOK
	}
	if a.busy == p.key() {
		v.busy, v.busyText = true, a.job
		v.got, v.total = a.cnt.got.Load(), a.cnt.total.Load()
		v.short, v.dot = tr("скачиваю…"), kindNew
	}
	v.info = []string{tr("Установка только в Interface\\AddOns. Папки определяются по .toc в архиве.")}
	if p.Asset == customSource {
		v.info = append(v.info, tr("Архив: исходники опубликованного релиза (ZIP)."))
	} else {
		name := p.Asset
		if c.customRel != nil {
			if asset, err := c.customRel.asset(p.Asset); err == nil {
				name = asset.Name
			}
		}
		v.info = append(v.info, tr("Архив: ")+name)
	}
	if len(st.Folders) > 0 {
		v.info = append(v.info, tr("Папки: ")+strings.Join(st.Folders, ", "))
	}
	return v
}

func (a *app) showNews(startup bool) {
	a.mu.Lock()
	if a.busy == "" && (!startup || a.cfg.SeenNews != customNews) {
		a.page = pageNews
	}
	a.mu.Unlock()
	a.changed()
}

func (a *app) closeNews(add bool) {
	a.mu.Lock()
	cfg := loadConfig()
	cfg.SeenNews = customNews
	if err := writeCustomConfig(cfg); err != nil {
		a.notice = fmt.Sprintf(tr("Не удалось сохранить: %v"), err)
	}
	a.cfg, a.page = cfg, pageMain
	a.mu.Unlock()
	if add {
		a.openCustom()
	}
	a.changed()
}

func checkCustomAddons(addons string, st *autoConfig) bool {
	failed := false
	for _, p := range loadConfig().Custom {
		if !customInstalled(addons, p) {
			st.drop(p.key())
			continue
		}
		rel, err := findCustomRelease(p.Repo)
		if err != nil {
			st.note(p.key(), err.Error())
			failed = true
			continue
		}
		if _, err := rel.asset(p.Asset); err != nil {
			st.note(p.key(), err.Error())
			st.drop(p.key())
			continue
		}
		if p.current(addons, rel.Tag) {
			st.drop(p.key())
			continue
		}
		st.setPending(p.key(), rel.Tag, "")
	}
	return failed
}

func installCustomPending(addons string, p pendingAddon, cnt *counter) (string, error) {
	project, ok := customByKey(loadConfig(), p.Name)
	if !ok || !customInstalled(addons, project) {
		return "", errUpToDate
	}
	if err := gameClosed(); err != nil {
		return "", err
	}
	rel, err := findCustomRelease(project.Repo)
	if err != nil {
		return "", err
	}
	if project.current(addons, rel.Tag) {
		return "", errUpToDate
	}
	if rel.Tag != p.Tag {
		return "", errors.New(tr("релиз сменился — проверю проект заново в следующий проход"))
	}
	return installCustom(addons, project, rel, cnt)
}
