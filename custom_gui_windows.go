package main

import (
	"strconv"
	"strings"
)

func (u *gui) drawCustom(x, y, w int32) int32 {
	d := u.app.customDraftView()
	u.line(box{x: x, y: y, w: w, h: u.s(34)}, tr("Добавить свой аддон"), "title", cBright)
	y += u.s(44)
	y += u.wrap(x, y, w,
		tr("Вставьте ссылку на публичный проект GitHub. Подойдёт главная страница, ссылка на релиз, ветку или файл — найдём последний стабильный релиз сами."),
		"body", cMuted) + u.s(20)
	u.line(box{x: x, y: y, w: w, h: u.s(22)}, tr("Ссылка на проект"), "bold", cText)
	y += u.s(28)
	u.customField(box{x: x, y: y, w: w, h: u.s(42)}, !d.Loading)
	y += u.s(54)
	label := tr("Найти релиз")
	if d.Loading {
		label = tr("Ищу релиз…")
	}
	u.button(box{x: x, y: y, w: u.btnWidth(label), h: u.s(36)},
		label, "customfind", !d.Loading && strings.TrimSpace(u.customText) != "", true)
	y += u.s(50)
	if d.Err != "" {
		y = u.notice(x, y, w, d.Err, kindErr) + u.s(12)
	}
	if d.Release != nil {
		u.line(box{x: x, y: y, w: w, h: u.s(24)}, d.Repo+" · "+d.Release.Tag, "head", cBright)
		y += u.s(32)
		y += u.wrap(x, y, w, tr("Выберите архив для вашей версии WoW. Этот вариант будет использоваться и при автообновлении."), "body", cMuted) + u.s(12)
		for i, a := range d.Release.Assets {
			label := a.Name
			if a.Name == customSource {
				label = tr("Исходники этого релиза (ZIP)")
			}
			if i == d.Selected {
				label = "● " + label
			} else {
				label = "○ " + label
			}
			u.button(box{x: x, y: y, w: w, h: u.s(36)},
				label, "customasset:"+strconv.Itoa(i), !d.Loading, i == d.Selected)
			y += u.s(42)
		}
		y += u.s(8)
		u.button(box{x: x, y: y, w: u.btnWidth(tr("Добавить проект")), h: u.s(38)},
			tr("Добавить проект"), "customadd", !d.Loading, true)
		y += u.s(54)
		y += u.wrap(x, y, w, tr("Проект появится слева. Нажмите «Установить» в его карточке, чтобы скачать и проверить содержимое архива."), "small", cMuted) + u.s(16)
	}
	y += u.wrap(x, y, w,
		tr("Только аддоны WoW с .toc. Форматы: ZIP, 7z, RAR, TAR, TAR.GZ, TAR.BZ2 и TAR.XZ. Установка — только в выбранную Interface\\AddOns; папки определяются из архива. Совместимость с вашей версией WoW уточняйте у автора."),
		"body", cMuted) + u.s(20)
	u.button(box{x: x, y: y, w: u.btnWidth(tr("Вставить пример: DefileAlert")), h: u.s(34)},
		tr("Вставить пример: DefileAlert"), "customexample", !d.Loading, false)
	return y + u.s(44)
}

func (u *gui) customField(b box, on bool) {
	u.add(b, "customfield", on)
	edge := uint32(cEdge)
	if u.customFocus {
		edge = cGoldEdge
	}
	u.framed(b, u.s(7), cBg, edge)
	in := box{x: b.x + u.s(12), y: b.y, w: b.w - u.s(24), h: b.h}
	if u.customText == "" {
		u.line(in, tr("https://github.com/владелец/проект"), "body", cMuted)
		return
	}
	if u.customSelected {
		u.round(in, u.s(3), cGoldBg)
	}
	r := []rune(u.customText)
	lead := ""
	for len(r) > 0 {
		if tw, _ := u.size(lead+string(r), "body", 0); tw <= in.w-u.s(5) {
			break
		}
		r, lead = r[1:], "…"
	}
	text := lead + string(r)
	u.line(in, text, "body", cText)
	if u.customFocus {
		tw, _ := u.size(text, "body", 0)
		u.fill(box{x: in.x + tw + u.s(1), y: b.y + u.s(11), w: max(1, u.s(1)), h: b.h - u.s(22)}, cGold)
	}
}

func (u *gui) customKey(c rune) {
	if u.app.customDraftView().Loading {
		return
	}
	if c == 1 {
		u.customSelected = true
		return
	}
	if c < 0x20 && c != 8 && c != 0x16 {
		return
	}
	if u.customSelected {
		u.customText, u.customSelected = "", false
	}
	switch {
	case c == 8:
		if r := []rune(u.customText); len(r) > 0 {
			u.customText = string(r[:len(r)-1])
		}
	case c == 0x16:
		u.customText += strings.TrimSpace(u.clipText())
	case c >= 0x20 && c < 0xD800:
		u.customText += string(c)
	}
	if r := []rune(u.customText); len(r) > 2048 {
		u.customText = string(r[:2048])
	}
	u.app.clearCustomDraft()
}

func (u *gui) customPanel(c cardView, x, y, w int32, idle bool) int32 {
	for _, info := range c.info {
		y += u.wrap(x, y, w, info, "body", cMuted) + u.s(12)
	}
	y += u.s(12)
	u.button(box{x: x, y: y, w: u.btnWidth(tr("Убрать из списка")), h: u.s(34)},
		tr("Убрать из списка"), "customremove:"+c.key, idle, false)
	bw := u.btnWidth(tr("Убрать из списка")) + u.s(12)
	u.button(box{x: x + bw, y: y, w: u.btnWidth(tr("Сменить архив")), h: u.s(34)},
		tr("Сменить архив"), "customedit:"+c.key, idle, false)
	y += u.s(46)
	y += u.wrap(x, y, w, tr("Удаляется только запись в обновлялке. Установленные файлы остаются в AddOns."), "small", cMuted)
	return y + u.s(12)
}

func (u *gui) drawNews(x, y, w int32) int32 {
	u.line(box{x: x, y: y, w: w, h: u.s(34)}, tr("Что нового"), "title", cBright)
	y += u.s(50)
	u.line(box{x: x, y: y, w: w, h: u.s(28)}, tr("Ваши аддоны с GitHub"), "head", cGold)
	y += u.s(38)
	for _, text := range []string{
		tr("Вы можете добавлять свои публичные GitHub-проекты с аддонами WoW и обновлять их через ManacodeUpdate."),
		tr("1. Нажмите «+ Свой GitHub-проект» над списком аддонов и вставьте ссылку на проект. Ссылка на страницу релизов тоже подойдёт."),
		tr("2. Нажмите «Найти релиз». Мы найдём последний стабильный релиз. Выберите архив для вашей версии WoW и нажмите «Добавить проект»."),
		tr("3. В новой карточке нажмите «Установить». Папки аддона определятся по .toc в архиве и попадут только в Interface\\AddOns. Перед установкой закройте игру полностью."),
		tr("Добавленные и установленные проекты обновляются в фоне вместе с обычными аддонами, когда включено автообновление."),
		tr("ZIP, 7z, RAR и TAR (в том числе .tar.gz, .tar.bz2 и .tar.xz). Если готового архива нет, можно выбрать исходники опубликованного релиза. Проекты без стабильных релизов не поддерживаются."),
		tr("Пример: github.com/suprepupre/DefileAlert — вставьте эту ссылку в поле добавления проекта."),
	} {
		y += u.wrap(x, y, w, text, "body", cText) + u.s(16)
	}
	y += u.s(6)
	bw := u.btnWidth(tr("Добавить свой проект"))
	u.button(box{x: x, y: y, w: bw, h: u.s(38)}, tr("Добавить свой проект"), "newsadd", true, true)
	u.button(box{x: x + bw + u.s(12), y: y, w: u.btnWidth(tr("Понятно")), h: u.s(38)},
		tr("Понятно"), "newsclose", true, false)
	return y + u.s(50)
}
