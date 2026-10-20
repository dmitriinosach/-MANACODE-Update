package main

import (
	"strconv"
	"strings"
	"time"
)

func marketWhen(unix int64) string {
	if unix <= 0 {
		return ""
	}
	t, now := time.Unix(unix, 0), time.Now()
	if t.YearDay() == now.YearDay() && t.Year() == now.Year() {
		return t.Format("15:04")
	}
	return t.Format("02.01 15:04")
}

func marketPeriod(days int) string {
	if days == 0 {
		return "за всё время"
	}
	return "за " + strconv.Itoa(days) + " дн."
}

func (u *gui) marketPanel(x, y, w int32) int32 {
	v := u.app.marketView()
	u.line(box{x, y, w, u.s(28)}, "Общая история аукциона", "head", cBright)
	y += u.s(36)
	if v.signedIn {
		y = u.marketLinked(v, x, y, w)
	} else {
		y = u.marketLogin(v, x, y, w)
	}
	y += u.s(12)
	if v.note != "" {
		kind := kindOK
		if v.noteErr {
			kind = kindErr
		}
		y = u.notice(x, y, w, v.note, kind) + u.s(6)
	}
	y += u.s(10)
	u.line(box{x, y, w, u.s(20)}, "Откуда брать сканы — папка аккаунта WoW", "small", cMuted)
	y += u.s(26)
	for i, account := range v.accounts {
		label := account
		if account == v.cfg.Account {
			label = "✓ " + label
		}
		u.button(box{x + int32(i%2)*(w+u.s(8))/2, y + int32(i/2)*u.s(42), (w - u.s(8)) / 2, u.s(36)}, label,
			"marketaccount:"+account, !v.busy, false)
	}
	y += int32((len(v.accounts)+1)/2) * u.s(42)
	if len(v.accounts) == 0 {
		y += u.wrap(x, y, w, "Сканов ещё нет: откройте аукцион в игре, нажмите «Скан» и выполните /reload.", "body", cMuted) + u.s(12)
	}
	y += u.s(10)
	u.line(box{x, y, w, u.s(20)}, "Сколько истории держать в аддоне", "small", cMuted)
	y += u.s(26)
	for i, days := range []int{7, 30, 90, 0} {
		label := strconv.Itoa(days) + " дн."
		if days == 0 {
			label = "Всё"
		}
		u.button(box{x + int32(i)*(w+u.s(8))/4, y, (w - u.s(24)) / 4, u.s(36)}, label,
			"marketdays:"+strconv.Itoa(days), !v.busy, days == v.cfg.Days)
	}
	y += u.s(50)
	lx := x + u.s(4)
	links := []struct{ label, id string }{{"Файл настроек обмена", "marketsettings"}}
	if v.signedIn {
		links = append(links, struct{ label, id string }{"Отключиться от базы", "marketlogout"})
	}
	for _, l := range links {
		lw, _ := u.size(l.label, "small", 0)
		b := box{lx, y, lw, u.s(20)}
		u.add(b, l.id, !v.busy)
		lc := uint32(cMuted)
		if u.hot(l.id) {
			lc = cGold
		}
		u.line(b, l.label, "small", lc)
		lx += lw + u.s(22)
	}
	y += u.s(30)
	y += u.wrap(x, y, w, "Цены и объёмы — наблюдения сканов; исчезновение лота не подтверждает выкуп.", "small", cMuted)
	return y + u.s(20)
}

func (u *gui) marketLinked(v marketView, x, y, w int32) int32 {
	pad := u.s(20)
	ix, iw := x+pad, w-2*pad
	h := u.s(228)
	u.round(box{x, y, w, h}, u.s(10), cPanel)
	top := y + u.s(16)
	dot, title, sub := uint32(cMuted), "Связь ещё не проверялась", ""
	switch {
	case v.busy:
		dot, title = cGold, "Обмен с базой рынка…"
	case v.checking:
		dot, title = cGold, "Проверяю связь с базой…"
	case v.st.Link == "ok":
		dot, title = cGreen, "Подключено к базе рынка"
	case v.st.Link == "offline" || v.st.Link == "error":
		dot, title = cRed, "Сервер рынка не отвечает"
	case v.st.Link == "denied":
		dot, title = cRed, "База отклонила доступ"
	}
	bw := u.s(150)
	u.dot(ix+u.s(5), top+u.s(13), u.s(5), dot)
	u.line(box{ix + u.s(20), top, iw - bw - u.s(28), u.s(26)}, title, "head", cBright)
	u.button(box{ix + iw - bw, top - u.s(2), bw, u.s(32)}, "Проверить связь", "marketcheck", !v.busy && !v.checking, false)
	parts := []string{}
	if v.cfg.Login != "" {
		parts = append(parts, "логин "+v.cfg.Login)
	}
	if v.cfg.Account != "" {
		parts = append(parts, "аккаунт "+v.cfg.Account)
	}
	if when := marketWhen(v.st.LinkAt); when != "" {
		if v.st.Link == "ok" {
			parts = append(parts, "сервер ответил в "+when)
		} else {
			parts = append(parts, "попытка в "+when)
		}
	}
	sub = strings.Join(parts, " · ")
	subColor := uint32(cMuted)
	if v.st.Link != "ok" && v.st.LinkNote != "" && !v.busy && !v.checking {
		reason := v.st.LinkNote
		for _, known := range []error{errMarketOffline, errMarketDenied} {
			reason = strings.TrimPrefix(reason, known.Error()+": ")
		}
		sub, subColor = reason+" · попытка в "+marketWhen(v.st.LinkAt), cRed
	}
	u.line(box{ix + u.s(20), top + u.s(28), iw - bw - u.s(28), u.s(20)}, sub, "small", subColor)
	ry := top + u.s(62)
	if v.busy || v.checking {
		u.progress(box{ix, ry - u.s(8), iw, u.s(4)}, 0, 0)
	} else {
		u.fill(box{ix, ry - u.s(8), iw, max(u.s(1), 1)}, cLine)
	}
	scans := "ещё не отправлялись"
	if v.st.PushAt > 0 {
		scans = "новых не было · проверено в " + marketWhen(v.st.PushAt)
		if v.st.Sent > 0 {
			scans = "отправлено " + num(v.st.Sent) + " " + ruPlural(v.st.Sent, "скан", "скана", "сканов") + " в " + marketWhen(v.st.PushAt)
		}
	}
	history := "ещё не загружалась"
	if v.st.PullAt > 0 {
		history = num(v.st.Total) + " " + ruPlural(v.st.Total, "наблюдение", "наблюдения", "наблюдений") + " " + marketPeriod(v.cfg.Days) +
			" · новых " + num(v.st.Added) + " · обновлено в " + marketWhen(v.st.PullAt)
	}
	game, gameColor := "—", uint32(cMuted)
	switch {
	case v.st.Pending && v.game:
		game, gameColor = "новая история ждёт закрытия игры", cGold
	case v.st.Pending:
		game, gameColor = "записываю в аддон…", cGold
	case v.st.InstalledAt > 0:
		game, gameColor = "записана в аддон в "+marketWhen(v.st.InstalledAt)+" — подхватится при запуске игры", cText
	case v.st.PullAt > 0:
		game, gameColor = "в аддоне уже последняя история", cText
	}
	for _, row := range []struct {
		label, value string
		color        uint32
	}{{"Сканы", scans, cText}, {"История", history, cText}, {"В игре", game, gameColor}} {
		u.line(box{ix, ry, u.s(90), u.s(22)}, row.label, "small", cMuted)
		u.line(box{ix + u.s(96), ry, iw - u.s(96), u.s(22)}, row.value, "body", row.color)
		ry += u.s(28)
	}
	ry += u.s(10)
	sw := u.s(206)
	label := "Синхронизировать сейчас"
	if v.busy {
		label = "Идёт обмен…"
	}
	u.button(box{ix, ry, sw, u.s(36)}, label, "marketsync", !v.busy, true)
	tg := box{ix + sw + u.s(24), ry + u.s(7), u.s(40), u.s(22)}
	u.switcher(tg, "marketauto", v.cfg.Automatic, !v.busy)
	tx := tg.x + tg.w + u.s(12)
	u.add(box{tx, ry, u.s(110), u.s(36)}, "marketauto", !v.busy)
	u.line(box{tx, ry, iw - (tx - ix), u.s(18)}, "Автообмен", "bold", cBright)
	hint := "выключен — только по кнопке"
	if v.cfg.Automatic {
		hint = "история каждые 2 мин, сканы — после /reload"
	}
	u.line(box{tx, ry + u.s(18), iw - (tx - ix), u.s(18)}, hint, "small", cMuted)
	return y + h
}

func (u *gui) marketLogin(v marketView, x, y, w int32) int32 {
	pad := u.s(20)
	ix, iw := x+pad, w-2*pad
	desc := "Логин и пароль выдаёт владелец базы рынка. Это не аккаунт WoW или Circle: пароль от игры не нужен."
	_, dh := u.size(desc, "small", iw)
	h := u.s(16) + u.s(30) + dh + u.s(14) + u.s(36) + u.s(20)
	u.round(box{x, y, w, h}, u.s(10), cPanel)
	top := y + u.s(16)
	dot, title := uint32(cRed), "Не подключено к базе рынка"
	if v.busy {
		dot, title = cGold, "Подключаюсь к базе рынка…"
	}
	u.dot(ix+u.s(5), top+u.s(13), u.s(5), dot)
	u.line(box{ix + u.s(20), top, iw - u.s(20), u.s(26)}, title, "head", cBright)
	ty := top + u.s(30)
	u.text(box{ix, ty, iw, dh + u.s(2)}, desc, "small", cMuted, dtLeft|dtWordBreak)
	fy := ty + dh + u.s(14)
	if !u.marketInit {
		u.marketInit = true
		u.marketLoginText = v.cfg.Login
	}
	bw := u.s(130)
	fw := (iw - bw - u.s(16)) / 2
	u.marketField(box{ix, fy, fw, u.s(36)}, "marketloginfield", u.marketLoginText, "Логин БД", !v.busy)
	u.marketField(box{ix + fw + u.s(8), fy, fw, u.s(36)}, "marketpassfield", strings.Repeat("•", len([]rune(u.marketPassText))), "Пароль БД", !v.busy)
	loginOn := !v.busy && strings.TrimSpace(u.marketLoginText) != "" && u.marketPassText != ""
	u.button(box{ix + iw - bw, fy, bw, u.s(36)}, "Подключить", "marketlogin", loginOn, true)
	return y + h
}

func (u *gui) switcher(tg box, id string, on, enabled bool) {
	u.add(tg, id, enabled)
	cy := tg.y + tg.h/2
	if on {
		c := uint32(cGold)
		if u.hot(id) {
			c = cGoldHi
		}
		u.round(tg, tg.h/2, c)
		u.dot(tg.x+tg.w-tg.h/2, cy, tg.h/2-u.s(4), cOnGold)
		return
	}
	bg := uint32(cEdge)
	if u.hot(id) {
		bg = 0x4A4A54
	}
	u.round(tg, tg.h/2, bg)
	u.dot(tg.x+tg.h/2, cy, tg.h/2-u.s(4), cKnob)
}

func (u *gui) marketField(b box, id, value, placeholder string, enabled bool) {
	u.add(b, id, enabled)
	edge := uint32(cEdge)
	focused := u.marketFocus == id
	switch {
	case focused:
		edge = cGold
	case u.hot(id):
		edge = cGoldEdge
	}
	u.framed(b, u.s(7), cBg, edge)
	in := box{b.x + u.s(12), b.y, b.w - u.s(24), b.h}
	color := uint32(cText)
	shown := value
	if value == "" && !focused {
		shown, color = placeholder, cMuted
	}
	u.line(in, shown, "body", color)
	if focused && u.tick/8%2 == 0 {
		tw, _ := u.size(value, "body", 0)
		if value == "" {
			tw = 0
		}
		cx := min(in.x+tw+u.s(1), in.x+in.w)
		u.fill(box{cx, b.y + u.s(9), max(u.s(1), 1), b.h - u.s(18)}, cGold)
	}
}

func (u *gui) marketKey(c rune) {
	value := &u.marketLoginText
	if u.marketFocus == "marketpassfield" {
		value = &u.marketPassText
	}
	switch {
	case c == 8:
		r := []rune(*value)
		if len(r) > 0 {
			*value = string(r[:len(r)-1])
		}
	case c == 0x7f:
		*value = ""
	case c == 0x16:
		*value += strings.TrimRight(u.clipText(), "\r\n")
	case c >= 0x20 && (c < 0xd800 || c > 0xdfff):
		*value += string(c)
	}
	r := []rune(*value)
	if len(r) > 256 {
		*value = string(r[:256])
	}
}

func (u *gui) submitMarketLogin() {
	if strings.TrimSpace(u.marketLoginText) == "" || u.marketPassText == "" {
		if strings.TrimSpace(u.marketLoginText) == "" {
			u.marketFocus = "marketloginfield"
		} else {
			u.marketFocus = "marketpassfield"
		}
		return
	}
	password := u.marketPassText
	u.marketPassText, u.marketFocus = "", ""
	u.app.loginMarket(strings.TrimSpace(u.marketLoginText), password)
}
