package main

import (
	"path/filepath"
	"strings"
)

const (
	dcHint     = "Вебхук задан — новые итоги уйдут в Discord сами."
	dcNoHook   = "Вебхук не задан — итоги сохраняются картинкой в Screenshots\\" + discordPicDir + "."
	dcLogsBtn  = "Открыть журналы"
	dcLogsHint = "Если итог не уходит в Discord — пришлите discord.log"
)

type discordView struct {
	hook    string
	divOn   bool
	divFile string
	status  string
	kind    int
	againOn bool
	saveOn  bool
}

func (a *app) discordPoll() {
	a.mu.Lock()
	if a.addons == "" || a.dcBusy {
		a.mu.Unlock()
		return
	}
	a.dcBusy = true
	addons, w := a.addons, a.dc
	a.mu.Unlock()
	go func() {
		res := w.poll(addons)
		a.mu.Lock()
		a.dcBusy = false
		if len(res) > 0 {
			r := res[len(res)-1]
			a.dcNote, a.dcKind = r.text(), kindOK
			if r.err != nil {
				a.dcKind = kindErr
			}
		}
		a.mu.Unlock()
		if len(res) > 0 {
			a.changed()
		}
	}()
}

func (a *app) discordAgain() {
	a.mu.Lock()
	if a.addons == "" || a.dcBusy {
		a.mu.Unlock()
		return
	}
	a.dcBusy = true
	a.dcNote, a.dcKind = "Discord: рисую и отправляю…", kindNone
	addons, w := a.addons, a.dc
	a.mu.Unlock()
	a.changed()
	go func() {
		r, ok := w.again(addons)
		a.mu.Lock()
		a.dcBusy = false
		switch {
		case !ok:
			a.dcNote, a.dcKind = "В игре нет итогов: сводка рейда → «В Discord», затем /reload", kindErr
		case r.err != nil:
			a.dcNote, a.dcKind = r.text(), kindErr
		default:
			a.dcNote, a.dcKind = r.text(), kindOK
		}
		a.mu.Unlock()
		a.changed()
	}()
}

func (a *app) saveHook(url string) bool {
	url = strings.TrimSpace(url)
	err := setWebhook(url)
	a.mu.Lock()
	switch {
	case err != nil:
		a.dcNote, a.dcKind = "Вебхук не сохранён: "+err.Error(), kindErr
	case url == "":
		a.hook = ""
		a.dcNote, a.dcKind = "Вебхук убран — итоги сохраняются картинкой в Screenshots\\"+discordPicDir, kindOK
	default:
		a.hook = url
		a.dcNote, a.dcKind = "Вебхук сохранён — новые итоги уйдут в Discord", kindOK
	}
	a.mu.Unlock()
	a.changed()
	return err == nil
}

func (a *app) toggleDivider() {
	a.mu.Lock()
	on := a.divOff
	a.mu.Unlock()
	err := setDividerOn(on)
	a.mu.Lock()
	switch {
	case err != nil:
		a.dcNote, a.dcKind = "Настройка не сохранена: "+err.Error(), kindErr
	case on:
		a.divOff = false
		a.dcNote, a.dcKind = "Разделитель включён — уйдёт после каждого итога", kindOK
	default:
		a.divOff = true
		a.dcNote, a.dcKind = "Разделитель выключен", kindOK
	}
	a.mu.Unlock()
	a.changed()
}

func (a *app) setDivider(file string) {
	err := setDividerFile(file)
	a.mu.Lock()
	switch {
	case err != nil:
		a.dcNote, a.dcKind = "Картинка разделителя не выбрана: "+err.Error(), kindErr
	case file == "":
		a.divFile = ""
		a.dcNote, a.dcKind = "Разделитель — встроенная картинка", kindOK
	default:
		a.divFile = file
		a.dcNote, a.dcKind = "Разделитель — "+filepath.Base(file), kindOK
	}
	a.mu.Unlock()
	a.changed()
}

func (a *app) discordView() discordView {
	a.mu.Lock()
	defer a.mu.Unlock()
	v := discordView{hook: a.hook, divOn: !a.divOff, divFile: a.divFile, status: a.dcNote, kind: a.dcKind, againOn: a.addons != "" && !a.dcBusy, saveOn: !a.dcBusy}
	if v.status == "" {
		v.status = dcHint
		if a.hook == "" {
			v.status = dcNoHook
		}
	}
	return v
}

func maskHook(s string) string {
	const mark = "/api/webhooks/"
	i := strings.Index(s, mark)
	if i < 0 {
		return s
	}
	rest := s[i+len(mark):]
	j := strings.IndexByte(rest, '/')
	if j < 0 {
		return s
	}
	token := rest[j+1:]
	if token == "" {
		return s
	}
	return s[:i+len(mark)+j+1] + strings.Repeat("•", min(len(token), 12))
}
