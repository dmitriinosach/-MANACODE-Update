package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type removalPlan struct {
	root    string
	key     string
	title   string
	folders []string
}

var renameRemoval = os.Rename

func planAddonRemoval(addons, key string) (removalPlan, error) {
	plan := removalPlan{root: realPath(addons), key: key, folders: []string{}}
	names := []string{}
	for _, d := range catalog {
		if d.key == key {
			plan.title = d.title
			names = append(names, d.name)
			if d.kind == kindRH {
				for _, pack := range roomPacks {
					names = append(names, packName(pack.code))
				}
			}
			break
		}
	}
	cfg := loadConfig()
	if p, ok := customByKey(cfg, key); ok {
		plan.title = p.title()
		names = append(names, p.installed(addons).Folders...)
	}
	if plan.title == "" {
		return plan, errors.New(tr("Аддон не найден в списке."))
	}
	seen := map[string]bool{}
	for _, name := range names {
		if seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true
		if strings.HasPrefix(key, "github/") {
			_, native := defByName(name)
			reserved := native || strings.EqualFold(name, homeSub)
			for _, pack := range roomPacks {
				reserved = reserved || strings.EqualFold(name, packName(pack.code))
			}
			if reserved {
				return plan, fmt.Errorf(tr("Папка %s принадлежит встроенному аддону."), name)
			}
		}
		target, err := customTarget(addons, name)
		if err != nil {
			return plan, err
		}
		if _, err := os.Lstat(target); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return plan, err
		}
		if devFolder(target) {
			return plan, fmt.Errorf(tr("%s — папка разработки, удаление запрещено."), name)
		}
		if selfInside(target) {
			return plan, errors.New(tr("Запустите обновлялку вне папки удаляемого аддона."))
		}
		for _, other := range cfg.Custom {
			if other.key() == key {
				continue
			}
			for _, owned := range other.installed(addons).Folders {
				if strings.EqualFold(name, owned) {
					return plan, fmt.Errorf(tr("Папка %s используется другим проектом: %s."), name, other.title())
				}
			}
		}
		plan.folders = append(plan.folders, target)
	}
	if len(plan.folders) == 0 {
		return plan, errors.New(tr("Файлы аддона не найдены."))
	}
	plan.root = filepath.Dir(plan.folders[0])
	return plan, nil
}

func removeAddon(addons, key string) (string, error) {
	plan, err := planAddonRemoval(addons, key)
	if err != nil {
		return "", err
	}
	unlock, err := lockInstall(plan.root)
	if err != nil {
		return "", err
	}
	defer unlock()
	if err := gameClosed(); err != nil {
		return "", err
	}
	plan, err = planAddonRemoval(addons, key)
	if err != nil {
		return "", err
	}
	trash, err := os.MkdirTemp(plan.root, ".manacode-remove-")
	if err != nil {
		return "", err
	}
	moved := []string{}
	rollback := func() error {
		var failure error
		for i := len(moved) - 1; i >= 0; i-- {
			target := moved[i]
			if err := renameRemoval(filepath.Join(trash, filepath.Base(target)), target); err != nil {
				failure = errors.Join(failure, fmt.Errorf(tr("Не удалось вернуть %s. Файлы сохранены в %s: %w"), filepath.Base(target), trash, err))
			}
		}
		_ = os.Remove(trash)
		return failure
	}
	for _, target := range plan.folders {
		if err := renameRemoval(target, filepath.Join(trash, filepath.Base(target))); err != nil {
			return "", errors.Join(fmt.Errorf(tr("Не удалось удалить %s: %w"), filepath.Base(target), err), rollback())
		}
		moved = append(moved, target)
	}
	cfg := loadConfig()
	if cfg.Auto != nil {
		cfg.Auto.drop(key)
		for _, target := range plan.folders {
			cfg.Auto.drop(filepath.Base(target))
		}
	}
	for i := range cfg.Custom {
		if cfg.Custom[i].key() == key {
			delete(cfg.Custom[i].Installs, customRootKey(addons))
		}
	}
	if err := writeCustomConfig(cfg); err != nil {
		return "", errors.Join(err, rollback())
	}
	if err := os.RemoveAll(trash); err != nil {
		return "", fmt.Errorf(tr("Аддон удалён, но не удалось очистить %s: %w"), trash, err)
	}
	return fmt.Sprintf(tr("%s удалён. Настройки в WTF сохранены."), plan.title), nil
}

func (a *app) removeAddon(key string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.busy != "" || a.marketBusy || a.lkBusy || a.laaBusy || a.dcBusy || a.addons == "" {
		return
	}
	addons := a.addons
	a.run(key, tr("Удаление аддона"), func() (string, error) {
		return removeAddon(addons, key)
	}, func(c *card, _ string, err error) {
		a.cfg = loadConfig()
		if p, ok := customByKey(a.cfg, key); ok {
			c.custom = &p
		}
	})
}
