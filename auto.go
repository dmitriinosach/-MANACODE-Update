package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

const (
	taskName     = "Manacode_Update"
	taskDesc     = "Manacode: обновление аддонов и данных"
	logFile      = "updater.log"
	logMax       = 256 << 10
	autoPhase    = 15 * time.Minute
	autoOnBoot   = "PT2M"
	addonHours   = 4
	addonRetry   = time.Hour
	logRepeat    = 24 * time.Hour
	noteData     = "data"
	noteGM       = "gm"
	noteTask     = "task"
	mainAddonLog = "аддон"
)

var (
	now      = time.Now
	schtasks = func(args ...string) ([]byte, error) {
		return hidden(exec.Command("schtasks", args...)).CombinedOutput()
	}
	errUpToDate = errors.New("уже стоит")
	reFormat    = regexp.MustCompile(`(?m)^## X-DataFormat:\s*(\d+)`)
	reCommand   = regexp.MustCompile(`<Command>([^<]*)</Command>`)
)

type autoConfig struct {
	AddonHours   int                `json:"addonHours,omitempty"`
	ETag         string             `json:"etag,omitempty"`
	LastModified string             `json:"lastModified,omitempty"`
	Baked        string             `json:"baked,omitempty"`
	GMBaked      string             `json:"gmBaked,omitempty"`
	GMKey        string             `json:"gmKeyId,omitempty"`
	AddonsAt     time.Time          `json:"addonsCheckedAt,omitzero"`
	AddonsFailed bool               `json:"addonsFailed,omitempty"`
	Pending      []pendingAddon     `json:"pending,omitempty"`
	Log          map[string]logMark `json:"log,omitempty"`
}

type pendingAddon struct {
	Name string `json:"name"`
	Tag  string `json:"tag"`
	Zip  string `json:"zip"`
}

type logMark struct {
	Text string    `json:"text"`
	At   time.Time `json:"at"`
}

func (c config) auto() autoConfig {
	if c.Auto == nil {
		return autoConfig{}
	}
	return *c.Auto
}

func (a autoConfig) hours() int {
	if a.AddonHours <= 0 {
		return addonHours
	}
	return a.AddonHours
}

func (a autoConfig) addonsDue(t time.Time) bool {
	if a.AddonsAt.IsZero() || t.Before(a.AddonsAt) {
		return true
	}
	wait := time.Duration(a.hours()) * time.Hour
	if a.AddonsFailed {
		wait = min(wait, addonRetry)
	}
	return !t.Before(a.AddonsAt.Add(wait))
}

func (a *autoConfig) setPending(name, tag, zip string) {
	a.drop(name)
	a.Pending = append(a.Pending, pendingAddon{Name: name, Tag: tag, Zip: zip})
}

func (a *autoConfig) drop(name string) {
	var keep []pendingAddon
	for _, p := range a.Pending {
		if p.Name != name {
			keep = append(keep, p)
		}
	}
	a.Pending = keep
}

func (a *autoConfig) note(dir, key, text string) {
	t := now()
	if m, ok := a.Log[key]; ok && m.Text == text && !t.Before(m.At) && t.Sub(m.At) < logRepeat {
		return
	}
	if a.Log == nil {
		a.Log = map[string]logMark{}
	}
	a.Log[key] = logMark{Text: text, At: t}
	autoLog(dir, "%s", text)
}

func storeAuto(dir string, a autoConfig) {
	cfg := loadConfig(dir)
	a.AddonHours = cfg.auto().AddonHours
	cfg.Auto = &a
	saveConfig(dir, cfg)
}

func setAddonHours(dir string, h int) {
	cfg := loadConfig(dir)
	a := cfg.auto()
	a.AddonHours = h
	cfg.Auto = &a
	saveConfig(dir, cfg)
}

func hoursRU(h int) string {
	if h == 24 {
		return "раз в сутки"
	}
	return fmt.Sprintf("раз в %d ч", h)
}

func autoPlan(h int) string {
	return "данные — каждые 15 мин, аддоны — " + hoursRU(h)
}

func localFormat(dir string) int {
	data := readToc(dir)
	if data == nil {
		return 0
	}
	if m := reFormat.FindSubmatch(data); m != nil {
		v, _ := strconv.Atoi(string(m[1]))
		return v
	}
	if m := reVersion.FindSubmatch(data); m != nil && !newer("0.15.0", string(m[1])) {
		return 12
	}
	return 11
}

func readToc(dir string) []byte {
	data, err := os.ReadFile(filepath.Join(dir, tocFile))
	if err != nil {
		return nil
	}
	return data
}

func formatMismatch(dir string, idx *index) error {
	want := localFormat(dir)
	if idx == nil || want == 0 || idx.V == want {
		return nil
	}
	return fmt.Errorf("на сервере данные формата v%d, а аддону нужен v%d — данные не трогаю, чтобы аддон не сломался", idx.V, want)
}

func autoLog(dir, format string, args ...any) {
	path := filepath.Join(cfgDir(dir), logFile)
	if st, err := os.Stat(path); err == nil && st.Size() > logMax {
		_ = os.Remove(path)
	}
	_ = os.MkdirAll(cfgDir(dir), 0o755)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s  %s\n", now().Format("02.01.2006 15:04:05"), fmt.Sprintf(format, args...))
}

func autoSeasons(cfg config, idx index) map[int]bool {
	all := seasonsOf(idx)
	want := map[int]bool{}
	for _, s := range cfg.Seasons {
		want[s] = true
	}
	sel := map[int]bool{}
	hit := false
	for _, s := range all {
		if want[s] {
			sel[s] = true
			hit = true
		}
	}
	if !hit {
		for _, s := range all {
			sel[s] = true
		}
	}
	sel[idx.Season] = true
	return sel
}

func localBaked(dir string) string {
	path := filepath.Join(dataDir(dir), playersFile)
	if _, err := os.Stat(path); err != nil {
		path = filepath.Join(dataDir(dir), mainFile)
	}
	if m := reBaked.FindStringSubmatch(tail(path, 4096)); m != nil {
		return m[1]
	}
	return ""
}

func gmKeyTag(dir string) string {
	if k := gmKey(dir); k != "" {
		return gmKeyID(k)
	}
	return ""
}

func priInstalled(dir string) bool {
	return readToc(dir) != nil && isDir(filepath.Join(dir, codeDir))
}

func runAuto(addons string, cnt *counter) int {
	dir := priDir(addons)
	cfg := loadConfig(dir)
	st := cfg.auto()
	if st.addonsDue(now()) {
		checkAddons(addons, cfg.beta(), &st)
		autoSelf(dir, &st)
		storeAuto(dir, st)
	}
	if len(st.Pending) > 0 {
		installPending(addons, &st, cnt)
		storeAuto(dir, st)
	}
	code := 0
	switch {
	case !priInstalled(dir):
		if st.AddonsFailed {
			code = 1
		}
	case localFormat(dir) >= formatV13:
		code = autoData13(dir, cfg, &st, cnt)
	default:
		code = autoData(dir, cfg, &st, cnt)
	}
	if moved, err := ensureTask(); err != nil {
		st.note(dir, noteTask, "задача Планировщика: "+err.Error())
	} else if moved {
		st.note(dir, noteTask, "задача Планировщика переведена на "+mustExe())
	}
	storeAuto(dir, st)
	return code
}

func mustExe() string {
	exe, _ := exePath()
	return exe
}

func checkAddons(addons string, beta bool, st *autoConfig) {
	dir := priDir(addons)
	failed := false
	for _, d := range catalog {
		a := d.at(addons)
		if a.localVersion() == "" || devFolder(a.folder) {
			st.drop(d.name)
			continue
		}
		if d.kind == kindRH {
			if !checkRH(dir, a, beta, st) {
				failed = true
			}
			continue
		}
		o := checkOther(a, false)
		switch {
		case o.none:
			st.drop(d.name)
		case o.tag == "":
			failed = true
			st.note(dir, d.name, "GitHub не ответил про релиз "+d.title)
		case o.newer && o.zip != "":
			st.setPending(d.name, o.tag, o.zip)
		default:
			st.drop(d.name)
		}
	}
	for _, p := range roomPacks {
		st.drop(packName(p.code))
	}
	st.AddonsAt, st.AddonsFailed = now(), failed
}

func checkRH(dir string, rh otherAddon, beta bool, st *autoConfig) bool {
	o := checkOther(rh, beta)
	arch, ok := o.archive(true)
	switch {
	case o.tag == "":
		st.note(dir, raidHelperName, "GitHub не ответил про релиз "+raidHelperTitle)
		return false
	case !ok:
		st.drop(raidHelperName)
	default:
		arch.probe()
		if p := planRH(o, arch, true, wantedPacks(loadConfig(dir)), false); p.empty() {
			st.drop(raidHelperName)
		} else {
			st.setPending(raidHelperName, p.tag, p.zip)
		}
	}
	return true
}

func installPending(addons string, st *autoConfig, cnt *counter) {
	dir := priDir(addons)
	var keep []pendingAddon
	for _, p := range st.Pending {
		title := p.Name
		if d, ok := defByName(p.Name); ok {
			title = d.title
		}
		text, err := installOne(addons, p, cnt)
		switch {
		case errors.Is(err, errUpToDate):
		case errors.Is(err, errGameRunning):
			keep = append(keep, p)
			st.note(dir, p.Name, fmt.Sprintf("%s %s ждёт: игра запущена — поставлю, когда её закроют", title, p.Tag))
		case errors.Is(err, errBusy):
			keep = append(keep, p)
			st.note(dir, p.Name, fmt.Sprintf("%s %s ждёт: другая обновлялка ставит аддон — повторю позже", title, p.Tag))
		case err != nil && text != "":
			st.note(dir, p.Name, fmt.Sprintf("%s; не вышло: %v", text, err))
		case err != nil:
			st.note(dir, p.Name, fmt.Sprintf("%s %s не поставлен: %v", title, p.Tag, err))
		case p.Name == raidHelperName:
			st.note(dir, p.Name, text)
		default:
			st.note(dir, p.Name, fmt.Sprintf("%s обновлён до %s", title, text))
		}
	}
	st.Pending = keep
}

func installOne(addons string, p pendingAddon, cnt *counter) (string, error) {
	d, ok := defByName(p.Name)
	if !ok {
		return "", errUpToDate
	}
	a := d.at(addons)
	local := a.localVersion()
	if local == "" || devFolder(a.folder) {
		return "", errUpToDate
	}
	switch d.kind {
	case kindPRI:
		if isDir(filepath.Join(a.folder, codeDir)) && !newer(p.Tag, local) {
			return "", errUpToDate
		}
		return updateAddon(a.folder, p.Zip, cnt)
	case kindRH:
		plan := pendingPlan(a, p.Tag, p.Zip, wantedPacks(loadConfig(priDir(addons))))
		if plan.empty() {
			return "", errUpToDate
		}
		res, err := runRH(a, plan, cnt)
		return res.text(), err
	}
	if !newer(p.Tag, local) {
		return "", errUpToDate
	}
	return installPlain(a, p.Zip, cnt)
}

func installPlain(a otherAddon, zipURL string, cnt *counter) (string, error) {
	if err := gameClosed(); err != nil {
		return "", err
	}
	cnt.file.Store(a.name)
	data, err := fetchAddonZip(zipURL, cnt)
	if err != nil {
		return "", err
	}
	files, err := unpackOther(a, data)
	if err != nil {
		return "", err
	}
	return installOther(a, files)
}

func autoData(dir string, cfg config, st *autoConfig, cnt *counter) int {
	baked, gmBaked, gmTag := localBaked(dir), gmLocalBaked(dir), gmKeyTag(dir)
	etag, lastMod := "", ""
	if baked != "" && baked == st.Baked && gmBaked == st.GMBaked && gmTag == st.GMKey {
		etag, lastMod = st.ETag, st.LastModified
	}
	res := fetchIndexIf(etag, lastMod)
	if res.err != nil {
		st.note(dir, noteData, "данные: "+res.err.Error())
		return 1
	}
	if res.same {
		st.note(dir, noteData, fmt.Sprintf("данные уже свежие (%s)", baked))
		return 0
	}
	st.ETag, st.LastModified = "", ""
	idx := res.idx
	if err := formatMismatch(dir, idx); err != nil {
		st.note(dir, noteData, "данные: "+err.Error())
		return 1
	}
	code, ok := 0, true
	if baked == idx.Baked {
		st.note(dir, noteData, fmt.Sprintf("данные уже свежие (%s)", idx.Baked))
	} else if written, err := download(*idx, dir, autoSeasons(cfg, *idx), cnt); err != nil {
		st.note(dir, noteData, "данные: "+err.Error())
		code, ok = 1, false
	} else {
		st.note(dir, noteData, fmt.Sprintf("данные обновлены до %s: %s", idx.Baked, strings.Join(written, ", ")))
	}
	if status, err := syncGM(dir, idx.GM, cnt); err != nil {
		st.note(dir, noteGM, "ГМ: "+err.Error())
		ok = false
	} else if status != "" {
		st.note(dir, noteGM, "ГМ: "+status)
	}
	if ok {
		st.ETag, st.LastModified = res.etag, res.lastMod
		st.Baked, st.GMBaked, st.GMKey = localBaked(dir), gmLocalBaked(dir), gmTag
	}
	return code
}

func xmlEscape(s string) string {
	var b bytes.Buffer
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func xmlUnescape(s string) string {
	return strings.NewReplacer("&lt;", "<", "&gt;", ">", "&quot;", `"`, "&apos;", "'", "&amp;", "&").Replace(s)
}

func taskXML(exe, userID, desc string) string {
	return `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo><Description>` + xmlEscape(desc) + `</Description></RegistrationInfo>
  <Triggers>
    <LogonTrigger><Enabled>true</Enabled><UserId>` + xmlEscape(userID) + `</UserId><Delay>` + autoOnBoot + `</Delay></LogonTrigger>
  </Triggers>
  <Principals><Principal id="Author"><UserId>` + xmlEscape(userID) + `</UserId><LogonType>InteractiveToken</LogonType><RunLevel>LeastPrivilege</RunLevel></Principal></Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <StartWhenAvailable>true</StartWhenAvailable>
    <RunOnlyIfNetworkAvailable>true</RunOnlyIfNetworkAvailable>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <Enabled>true</Enabled>
  </Settings>
  <Actions Context="Author">
    <Exec><Command>` + xmlEscape(exe) + `</Command><Arguments>-watch</Arguments><WorkingDirectory>` + xmlEscape(filepath.Dir(exe)) + `</WorkingDirectory></Exec>
  </Actions>
</Task>
`
}

func utf16File(s string) []byte {
	var b bytes.Buffer
	b.Write([]byte{0xFF, 0xFE})
	for _, u := range utf16.Encode([]rune(s)) {
		_ = binary.Write(&b, binary.LittleEndian, u)
	}
	return b.Bytes()
}

func decodeText(b []byte) string {
	if len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE {
		b = b[2:]
	} else if bytes.IndexByte(b, 0) < 0 {
		return string(b)
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(b[2*i:])
	}
	return string(utf16.Decode(u))
}

func taskCommand(xml []byte) string {
	if m := reCommand.FindStringSubmatch(decodeText(xml)); m != nil {
		return xmlUnescape(strings.Trim(m[1], `"`))
	}
	return ""
}

func autostartOn() bool {
	_, err := schtasks("/Query", "/TN", taskName)
	return err == nil
}

func ensureTask() (bool, error) {
	out, err := schtasks("/Query", "/TN", taskName, "/XML")
	if err != nil {
		return false, nil
	}
	cmd := taskCommand(out)
	if cmd != "" && strings.Contains(decodeText(out), "<Arguments>-watch</Arguments>") {
		if st, err := os.Stat(cmd); err == nil && !st.IsDir() && !isLegacyName(filepath.Base(cmd)) {
			return false, nil
		}
	}
	return true, setAutostart(true)
}

func setAutostart(on bool) error {
	if !on {
		if !autostartOn() {
			return nil
		}
		if out, err := schtasks("/Delete", "/TN", taskName, "/F"); err != nil {
			return fmt.Errorf("не смог убрать задачу: %s", strings.TrimSpace(decodeText(out)))
		}
		return nil
	}
	exe, err := exePath()
	if err != nil {
		return err
	}
	u, err := user.Current()
	if err != nil {
		return err
	}
	tmp := filepath.Join(os.TempDir(), taskName+".xml")
	if err := os.WriteFile(tmp, utf16File(taskXML(exe, u.Username, taskDesc)), 0o644); err != nil {
		return err
	}
	defer os.Remove(tmp)
	if out, err := schtasks("/Create", "/TN", taskName, "/XML", tmp, "/F"); err != nil {
		return fmt.Errorf("не смог создать задачу: %s", strings.TrimSpace(decodeText(out)))
	}
	_, _ = schtasks("/Run", "/TN", taskName)
	return nil
}

func runWatch(addons string, cnt *counter) int {
	unlock, ok := lockWatch(watchMutex)
	if !ok {
		return 0
	}
	exe, _ := exePath()
	var born time.Time
	if st, err := os.Stat(exe); err == nil {
		born = st.ModTime()
	}
	dc := newDiscordWatch()
	for {
		cleanOldExe()
		runAuto(addons, cnt)
		if st, err := os.Stat(exe); err == nil && !born.IsZero() && !st.ModTime().Equal(born) {
			autoLog("", "обновлялка обновилась — перезапускаюсь")
			unlock()
			if !hop(exe, []string{"-watch"}) {
				autoLog("", "не смог перезапуститься")
			}
			return 0
		}
		for end := time.Now().Add(autoPhase); time.Now().Before(end); time.Sleep(discordPhase) {
			discordAuto(addons, dc)
		}
	}
}

func autostartCLI(dir, arg string) int {
	switch arg {
	case "on", "off":
	default:
		fmt.Println("ОШИБКА: -autostart on|off")
		return 1
	}
	if err := setAutostart(arg == "on"); err != nil {
		fmt.Println("ОШИБКА:", err)
		return 1
	}
	if arg == "on" {
		fmt.Println("автообновление включено: при входе в Windows, " + autoPlan(loadConfig(dir).auto().hours()))
	} else {
		fmt.Println("автообновление выключено")
	}
	return 0
}
