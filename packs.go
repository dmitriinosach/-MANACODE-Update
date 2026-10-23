package main

import (
	"archive/zip"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type roomPack struct {
	code  string
	label string
}

var roomPacks = []roomPack{{"ICC", "ЦЛК"}, {"ULD", "Ульдуар"}, {"TOC", "ИВК"}, {"RS", "РС"}}

const liteInfix = "-lite-"

func packName(code string) string { return raidHelperName + "_" + code }

func packAddon(rhFolder, code string) otherAddon {
	return otherAddon{name: packName(code), folder: filepath.Join(filepath.Dir(rhFolder), packName(code))}
}

func packLabel(code string) string {
	for _, p := range roomPacks {
		if p.code == code {
			return p.label
		}
	}
	return code
}

func tagVersion(tag string) string { return strings.TrimPrefix(tag, "v") }

func packDev(folder string) bool {
	if !sameDir(realPath(folder), folder) {
		return true
	}
	return devFolder(folder)
}

func wantedPacks(c config) map[string]bool {
	out := map[string]bool{}
	if c.RaidHelper == nil || c.RaidHelper.Packs == nil {
		for _, p := range roomPacks {
			out[p.code] = true
		}
		return out
	}
	for _, code := range *c.RaidHelper.Packs {
		out[code] = true
	}
	return out
}

func savePacks(sel map[string]bool) {
	cfg := loadConfig()
	list := []string{}
	for _, p := range roomPacks {
		if sel[p.code] {
			list = append(list, p.code)
		}
	}
	cfg.rh().Packs = &list
	saveConfig(cfg)
}

type rhArchive struct {
	tag   string
	full  releaseAsset
	lite  releaseAsset
	packs map[string]int64
}

func archiveOf(r ghRelease) rhArchive {
	a := rhArchive{tag: r.Tag}
	for _, as := range r.Assets {
		switch {
		case !strings.HasSuffix(as.Name, ".zip"):
		case strings.HasPrefix(as.Name, raidHelperName+liteInfix):
			a.lite = as
		case strings.HasPrefix(as.Name, raidHelperName+"-") && a.full.URL == "":
			a.full = as
		}
	}
	return a
}

func (a *rhArchive) probe() {
	if a.full.URL == "" || a.full.Size <= 0 || a.packs != nil {
		return
	}
	zr, err := zip.NewReader(newRangeReader(a.full.URL, a.full.Size), a.full.Size)
	if err != nil {
		return
	}
	a.packs = packSizes(zr.File)
}

func packSizes(files []*zip.File) map[string]int64 {
	out := map[string]int64{}
	for _, f := range files {
		top, _, ok := strings.Cut(strings.ReplaceAll(f.Name, "\\", "/"), "/")
		code, pack := strings.CutPrefix(top, raidHelperName+"_")
		if ok && pack && code != "" {
			out[code] += int64(f.UncompressedSize64)
		}
	}
	return out
}

func releaseByVersion(list []ghRelease, version string) (ghRelease, bool) {
	for _, r := range list {
		if !r.Draft && version != "" && tagVersion(r.Tag) == version {
			return r, true
		}
	}
	return ghRelease{}, false
}

func (o otherRelease) archive(update bool) (rhArchive, bool) {
	if !update || !o.newer {
		if r, ok := releaseByVersion(o.all, o.local); ok {
			return archiveOf(r), true
		}
	}
	for _, r := range o.all {
		if o.tag != "" && r.Tag == o.tag {
			return archiveOf(r), true
		}
	}
	return rhArchive{}, false
}

type rhPlan struct {
	tag     string
	zip     string
	main    bool
	install []string
	remove  []string
}

func (p rhPlan) empty() bool { return !p.main && len(p.install) == 0 && len(p.remove) == 0 }

func planRH(o otherRelease, arch rhArchive, update bool, want map[string]bool, explicit bool) rhPlan {
	p := rhPlan{tag: arch.tag, main: update && o.newer && !o.dev && arch.tag == o.tag}
	ver := tagVersion(arch.tag)
	for _, rp := range roomPacks {
		pa := packAddon(o.addon.folder, rp.code)
		if packDev(pa.folder) {
			continue
		}
		local := pa.localVersion()
		if !want[rp.code] {
			if local != "" {
				p.remove = append(p.remove, rp.code)
			}
			continue
		}
		has := arch.packs != nil && arch.packs[rp.code] > 0
		maybe := arch.packs == nil && (explicit || local != "")
		if (has || maybe) && local != ver && arch.full.URL != "" && (o.local != "" || p.main) {
			p.install = append(p.install, rp.code)
		}
	}
	switch {
	case len(p.install) > 0:
		p.zip = arch.full.URL
	case p.main && arch.lite.URL != "":
		p.zip = arch.lite.URL
	case p.main:
		p.zip = arch.full.URL
	}
	return p
}

func liteURL(u string) bool {
	return strings.HasPrefix(filepath.Base(filepath.FromSlash(u)), raidHelperName+liteInfix)
}

func pendingPlan(rh otherAddon, tag, zipURL string, want map[string]bool) rhPlan {
	p := rhPlan{tag: tag, zip: zipURL}
	local := rh.localVersion()
	p.main = zipURL != "" && (local == "" || newer(tag, local))
	full := zipURL != "" && !liteURL(zipURL)
	for _, rp := range roomPacks {
		pa := packAddon(rh.folder, rp.code)
		if packDev(pa.folder) {
			continue
		}
		have := pa.localVersion()
		switch {
		case want[rp.code] && full && have != tagVersion(tag):
			p.install = append(p.install, rp.code)
		case !want[rp.code] && have != "":
			p.remove = append(p.remove, rp.code)
		}
	}
	if !p.main && len(p.install) == 0 {
		p.zip = ""
	}
	return p
}

func removePack(folder string) error {
	if packDev(folder) {
		return fmt.Errorf("%s — папка разработки, не трогаю", filepath.Base(folder))
	}
	if err := gameClosed(); err != nil {
		return err
	}
	unlock, err := lockInstall(filepath.Dir(folder))
	if err != nil {
		return err
	}
	defer unlock()
	if err := os.RemoveAll(folder); err != nil {
		return fmt.Errorf("папка %s занята — закройте игру и запустите обновлялку снова", filepath.Base(folder))
	}
	return nil
}

type rhResult struct {
	ver   string
	lines []string
}

func (r rhResult) text() string { return strings.Join(r.lines, "; ") }

func runRH(rh otherAddon, p rhPlan, cnt *counter) (rhResult, error) {
	var res rhResult
	if p.empty() {
		return res, nil
	}
	if devFolder(rh.folder) {
		return res, fmt.Errorf("%s — папка разработки, не трогаю", rh.name)
	}
	if err := gameClosed(); err != nil {
		return res, err
	}
	var data []byte
	if p.zip != "" {
		cnt.file.Store(rh.name)
		cnt.got.Store(0)
		cnt.total.Store(0)
		var err error
		if data, err = fetchAddonZip(p.zip, cnt); err != nil {
			return res, err
		}
	}
	return applyRH(rh, p, data)
}

func applyRH(rh otherAddon, p rhPlan, data []byte) (rhResult, error) {
	var res rhResult
	if devFolder(rh.folder) {
		return res, fmt.Errorf("%s — папка разработки, не трогаю", rh.name)
	}
	if p.main {
		files, err := unpackOther(rh, data)
		if err != nil {
			return res, err
		}
		ver, err := installOther(rh, files)
		if err != nil {
			return res, err
		}
		res.ver = ver
		res.lines = append(res.lines, raidHelperName+" обновлён до "+ver)
	}
	var errs []error
	for _, code := range p.install {
		pa := packAddon(rh.folder, code)
		files, err := unpackOther(pa, data)
		if errors.Is(err, errNotInArchive) {
			continue
		}
		if err == nil {
			_, err = installOther(pa, files)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("3D-залы %s: %w", packLabel(code), err))
			continue
		}
		res.lines = append(res.lines, "3D-залы "+packLabel(code)+" "+tagVersion(p.tag)+" поставлены")
	}
	for _, code := range p.remove {
		if err := removePack(packAddon(rh.folder, code).folder); err != nil {
			errs = append(errs, fmt.Errorf("3D-залы %s: %w", packLabel(code), err))
			continue
		}
		res.lines = append(res.lines, "3D-залы "+packLabel(code)+" удалены")
	}
	return res, errors.Join(errs...)
}

func syncRH(o otherRelease, update bool, want map[string]bool, cnt *counter) (rhResult, error) {
	if o.dev {
		return rhResult{}, fmt.Errorf("%s — папка разработки, не трогаю", o.addon.name)
	}
	arch, ok := o.archive(update)
	if !ok {
		return rhResult{}, fmt.Errorf("на GitHub нет релиза %s", raidHelperName)
	}
	arch.probe()
	return runRH(o.addon, planRH(o, arch, update, want, true), cnt)
}
