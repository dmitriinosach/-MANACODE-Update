package main

import (
	"path/filepath"
	"strings"
)

type addonKind int

const (
	kindPRI addonKind = iota
	kindRH
	kindPlain
)

const (
	arcadeName = "ManaCode_RaidWaitingArcade"
	buffName   = "ManaCode_LazyBuffBar"
	vendorName = "ManaCode_Vendor"
)

var (
	priRepo    = "dmitriinosach/-MANACODE-PlayerRaidsInfo"
	arcadeRepo = "dmitriinosach/-MANACODE-RaidWaitingArcade"
	buffRepo   = "dmitriinosach/-MANACODE-LazyBuffBar"
	vendorRepo = "dmitriinosach/-MANACODE-Vendor"
)

type addonDef struct {
	key   string
	name  string
	title string
	desc  string
	repo  *string
	kind  addonKind
	curse string
	warn  string
}

var catalog = []addonDef{
	{key: "pri", name: addonName, title: "PlayerRaidsInfo", desc: "Статистика рейдов игроков: окно /raids и Alt-карточка", repo: &priRepo, kind: kindPRI, warn: "Работает только на WoW Circle: статистика собрана из логов его серверов"},
	{key: "rh", name: raidHelperName, title: raidHelperTitle, desc: "Разбор рейда по записи и сборщик состава", repo: &raidHelperRepo, kind: kindRH},
	{key: "arc", name: arcadeName, title: "Raid Waiting Arcade", desc: "Мини-игры, пока собирается рейд", repo: &arcadeRepo, kind: kindPlain},
	{key: "buff", name: buffName, title: "LazyBuffBar", desc: "Панель бафов: что держать твоему классу и спеку", repo: &buffRepo, kind: kindPlain},
	{key: "vendor", name: vendorName, title: "Vendor", desc: "Окно торговца: весь товар одним списком", repo: &vendorRepo, kind: kindPlain},
}

func (d addonDef) at(addons string) otherAddon {
	return otherAddon{name: d.name, folder: folderIn(addons, d.name), repo: *d.repo}
}

func defByName(name string) (addonDef, bool) {
	for _, d := range catalog {
		if strings.EqualFold(d.name, name) {
			return d, true
		}
	}
	return addonDef{}, false
}

func priDir(addons string) string {
	return folderIn(addons, addonName)
}

func addonsOf(priFolder string) string {
	return filepath.Dir(priFolder)
}
