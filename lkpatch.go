package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	lkChunk   = `DBFilesClient\DungeonMapChunk.dbc`
	lkDMap    = `DBFilesClient\DungeonMap.dbc`
	lkMap     = 631
	lkFloor   = 109
	lkOld     = "patch-A.MPQ"
	lkOldSize = 12868
	lkMarks   = "lkpatch.json"
)

var (
	lkGroups = []uint32{28150, 28151}
	lkOldIDs = []uint32{1828, 1829}
	lkMu     sync.Mutex
)

const (
	lkNone = iota
	lkHave
	lkSkip
	lkPut
)

type lkResult struct {
	kind int
	text string
}

type lkMark struct {
	Path    string `json:"path"`
	SHA     string `json:"sha256"`
	Base    string `json:"base"`
	BaseSHA string `json:"baseSha256"`
}

type mpqEntry struct {
	name string
	data []byte
}

func mpqEncrypt(b []uint32, key uint32) {
	seed := uint32(0xEEEEEEEE)
	for i := range b {
		seed += mpqCrypt[0x400+(key&0xFF)]
		plain := b[i]
		b[i] = plain ^ (key + seed)
		key = ((^key << 0x15) + 0x11111111) | (key >> 0x0B)
		seed = plain + seed + (seed << 5) + 3
	}
}

func writeMPQ(files ...mpqEntry) []byte {
	var list []string
	for _, f := range files {
		list = append(list, f.name)
	}
	files = append(files, mpqEntry{"(listfile)", []byte(strings.Join(list, "\r\n") + "\r\n")})
	const head = 44
	n := uint32(16)
	for n < uint32(len(files))*2 {
		n *= 2
	}
	hashes := make([]uint32, n*4)
	for i := range hashes {
		hashes[i] = 0xFFFFFFFF
	}
	blocks := make([]uint32, len(files)*4)
	var body []byte
	pos := uint32(head)
	for bi, f := range files {
		blocks[bi*4], blocks[bi*4+1], blocks[bi*4+2], blocks[bi*4+3] = pos, uint32(len(f.data)), uint32(len(f.data)), 0x80000000
		body = append(body, f.data...)
		pos += uint32(len(f.data))
		start := mpqHash(f.name, 0) % n
		for k := uint32(0); k < n; k++ {
			e := hashes[((start+k)%n)*4:]
			if e[3] != 0xFFFFFFFF {
				continue
			}
			e[0], e[1], e[2], e[3] = mpqHash(f.name, 1), mpqHash(f.name, 2), 0, uint32(bi)
			break
		}
	}
	mpqEncrypt(hashes, mpqHash("(hash table)", 3))
	mpqEncrypt(blocks, mpqHash("(block table)", 3))
	hashPos := pos
	blockPos := hashPos + n*16
	out := make([]byte, head)
	copy(out, "MPQ\x1a")
	le := binary.LittleEndian
	le.PutUint32(out[4:], head)
	le.PutUint32(out[8:], blockPos+uint32(len(blocks))*4)
	le.PutUint16(out[12:], 1)
	le.PutUint16(out[14:], 3)
	le.PutUint32(out[16:], hashPos)
	le.PutUint32(out[20:], blockPos)
	le.PutUint32(out[24:], n)
	le.PutUint32(out[28:], uint32(len(files)))
	out = append(out, body...)
	for _, t := range [][]uint32{hashes, blocks} {
		for _, v := range t {
			out = le.AppendUint32(out, v)
		}
	}
	return out
}

func dbcRows(b []byte, fields uint32, what string) (rows [][]byte, strs []byte, err error) {
	le := binary.LittleEndian
	if len(b) < 20 || string(b[:4]) != "WDBC" {
		return nil, nil, errors.New(what + ": не WDBC")
	}
	recs, nf, size, sb := le.Uint32(b[4:]), le.Uint32(b[8:]), le.Uint32(b[12:]), le.Uint32(b[16:])
	if nf != fields || size != fields*4 || 20+uint64(recs)*uint64(size)+uint64(sb) != uint64(len(b)) {
		return nil, nil, errors.New(what + ": заголовок не сходится")
	}
	for i := uint32(0); i < recs; i++ {
		rows = append(rows, b[20+i*size:20+(i+1)*size])
	}
	return rows, b[20+recs*size:], nil
}

func chunkRows(b []byte) ([][]byte, []byte, error) {
	return dbcRows(b, 5, "DungeonMapChunk.dbc")
}

func lkMissing(rows [][]byte) []uint32 {
	le := binary.LittleEndian
	var miss []uint32
	for _, g := range lkGroups {
		found := false
		for _, r := range rows {
			if le.Uint32(r[4:]) == lkMap && le.Uint32(r[8:]) == g {
				found = true
				break
			}
		}
		if !found {
			miss = append(miss, g)
		}
	}
	return miss
}

func lkHasRows(b []byte) (bool, error) {
	rows, _, err := chunkRows(b)
	if err != nil {
		return false, err
	}
	return len(lkMissing(rows)) == 0, nil
}

func lkBuild(dbc []byte) ([]byte, error) {
	rows, strs, err := chunkRows(dbc)
	if err != nil {
		return nil, err
	}
	le := binary.LittleEndian
	top := uint32(0)
	for _, r := range rows {
		top = max(top, le.Uint32(r))
	}
	recs := append([][]byte{}, rows...)
	for i, g := range lkMissing(rows) {
		row := make([]byte, 0, 20)
		for _, v := range []uint32{top + 1 + uint32(i), lkMap, g, lkFloor, 0} {
			row = le.AppendUint32(row, v)
		}
		at := len(recs)
		for j, r := range recs {
			if m := le.Uint32(r[4:]); m > lkMap || m == lkMap && le.Uint32(r[8:]) > g {
				at = j
				break
			}
		}
		recs = append(recs[:at], append([][]byte{row}, recs[at:]...)...)
	}
	out := append([]byte{}, dbc[:20]...)
	le.PutUint32(out[4:], uint32(len(recs)))
	for _, r := range recs {
		out = append(out, r...)
	}
	return append(out, strs...), nil
}

func lkLast(load []mpqFile, name string) (mpqFile, []byte, map[string]bool) {
	var src mpqFile
	var data []byte
	with := map[string]bool{}
	for i := len(load) - 1; i >= 0; i-- {
		a, err := openMPQ(load[i].path)
		if err != nil {
			continue
		}
		if _, ok := a.find(name); ok {
			with[load[i].path] = true
			if data == nil {
				if b, err := a.read(name); err == nil {
					src, data = load[i], b
				}
			}
		}
		a.Close()
	}
	return src, data, with
}

func lkFloorCheck(game string, load []mpqFile) string {
	src, dbc, _ := lkLast(load, lkDMap)
	if dbc == nil {
		return fmt.Sprintf("DungeonMap.dbc нет ни в одном архиве игры — этаж %d не сверить", lkFloor)
	}
	rows, _, err := dbcRows(dbc, 8, "DungeonMap.dbc")
	if err != nil {
		return gameRel(game, src.path) + ": " + err.Error()
	}
	le := binary.LittleEndian
	for _, r := range rows {
		if le.Uint32(r) != lkFloor {
			continue
		}
		if m := le.Uint32(r[4:]); m != lkMap {
			return fmt.Sprintf("в %s этаж %d — карта %d, а не ЦЛК (%d)", gameRel(game, src.path), lkFloor, m, lkMap)
		}
		return ""
	}
	return fmt.Sprintf("в %s нет этажа %d", gameRel(game, src.path), lkFloor)
}

func lkLetter(ldir, loc, free string, load []mpqFile, with map[string]bool) (string, string) {
	top, topFile := "", ""
	for _, f := range load {
		if l := f.letter(); l != "" && f.loc && with[f.path] && l > top {
			top, topFile = l, f.path
		}
	}
	ents, _ := os.ReadDir(ldir)
	taken := func(name string) bool {
		for _, e := range ents {
			if strings.EqualFold(e.Name(), name) && !strings.EqualFold(e.Name(), free) {
				return true
			}
		}
		return false
	}
	start := byte('A')
	if top != "" {
		start = top[0] + 1
	}
	for c := start; c <= 'Z'; c++ {
		if name := "patch-" + loc + "-" + string(c) + ".MPQ"; !taken(name) {
			return name, ""
		}
	}
	return "", topFile
}

func gameRel(game, p string) string {
	if r, err := filepath.Rel(game, p); err == nil {
		return r
	}
	return p
}

func loadLKMarks() []lkMark {
	var m []lkMark
	if p := homePath(lkMarks); p != "" {
		if b, err := os.ReadFile(p); err == nil {
			_ = json.Unmarshal(b, &m)
		}
	}
	return m
}

func saveLKMarks(m []lkMark) {
	p := homePath(lkMarks)
	if p == "" {
		return
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = os.WriteFile(p, b, 0o644)
}

func lkOldOurs(p string) bool {
	st, err := os.Stat(p)
	if err != nil || st.Size() != lkOldSize {
		return false
	}
	a, err := openMPQ(p)
	if err != nil {
		return false
	}
	defer a.Close()
	files := 0
	for i := 0; i+3 < len(a.blocks); i += 4 {
		if a.blocks[i+3]&0x80000000 != 0 {
			files++
		}
	}
	dbc, err := a.read(lkChunk)
	if _, ok := a.find("(listfile)"); files != 2 || err != nil || !ok {
		return false
	}
	rows, _, err := chunkRows(dbc)
	if err != nil {
		return false
	}
	le := binary.LittleEndian
	hit := 0
	for _, r := range rows {
		for i, id := range lkOldIDs {
			if le.Uint32(r) == id && le.Uint32(r[4:]) == lkMap && le.Uint32(r[8:]) == lkGroups[i] && le.Uint32(r[12:]) == lkFloor {
				hit++
			}
		}
	}
	return hit == len(lkOldIDs)
}

const lkRunning = "Игра запущена — закройте её полностью (Wow.exe) и нажмите ещё раз."

func lkPatch(game string) (lkResult, error) {
	data := filepath.Join(game, "Data")
	if game == "" || !isDir(data) {
		return lkResult{lkSkip, "Клиент игры не найден: рядом с Interface\\AddOns нет папки Data."}, nil
	}
	locs := mpqLocales(data)
	if len(locs) == 0 {
		return lkResult{lkSkip, "Локаль клиента не найдена: в Data нет папки вроде ruRU с locale-ruRU.MPQ."}, nil
	}
	lkMu.Lock()
	defer lkMu.Unlock()
	var lines []string
	kind := lkNone
	ents, _ := os.ReadDir(data)
	for _, e := range ents {
		p := filepath.Join(data, e.Name())
		if !strings.EqualFold(e.Name(), lkOld) {
			continue
		}
		if !lkOldOurs(p) {
			lines = append(lines, gameRel(game, p)+" — не наш, не трогали.")
			continue
		}
		if gameRunning() {
			return lkResult{lkSkip, lkRunning}, nil
		}
		if err := os.Remove(p); err != nil {
			return lkResult{}, fmt.Errorf("не смог удалить старый %s: %w", gameRel(game, p), err)
		}
		lines, kind = append(lines, "Удалён старый "+gameRel(game, p)+" из прошлого релиза."), lkPut
	}
	marks := loadLKMarks()
	defer func() { saveLKMarks(marks) }()
	for _, loc := range locs {
		res, err := lkLocale(game, loc, &marks)
		if err != nil {
			return lkResult{}, err
		}
		if res.text == lkRunning {
			return res, nil
		}
		lines = append(lines, res.text)
		kind = max(kind, res.kind)
	}
	return lkResult{kind, strings.Join(lines, "\n")}, nil
}

func lkLocale(game, loc string, marks *[]lkMark) (lkResult, error) {
	data := filepath.Join(game, "Data")
	ldir := filepath.Join(data, loc)
	load := mpqLoad(data, loc)
	var mark lkMark
	note := ""
	var keep []lkMark
	for _, m := range *marks {
		switch {
		case !strings.EqualFold(filepath.Dir(m.Path), ldir):
			keep = append(keep, m)
		case mark.Path != "" || fileSHA(m.Path) == "":
		case fileSHA(m.Path) == m.SHA:
			mark = m
			keep = append(keep, m)
		default:
			note += gameRel(game, m.Path) + " изменён не нами — не трогали.\n"
		}
	}
	*marks = keep
	mine := mark.Path
	forget := func() {
		var rest []lkMark
		for _, m := range *marks {
			if m.Path != mine {
				rest = append(rest, m)
			}
		}
		*marks = rest
	}
	var others []mpqFile
	mineAt, srcAt := -1, -1
	for i, f := range load {
		if mine != "" && strings.EqualFold(f.path, mine) {
			mineAt = i
			continue
		}
		others = append(others, f)
	}
	src, dbc, with := lkLast(others, lkChunk)
	if dbc == nil {
		return lkResult{lkSkip, note + "Не поставлен: DungeonMapChunk.dbc нет ни в одном архиве игры."}, nil
	}
	for i, f := range load {
		if f.path == src.path {
			srcAt = i
		}
	}
	var hidden []string
	for _, f := range others {
		if with[f.path] && f.path != src.path && f.letter() != "" {
			hidden = append(hidden, gameRel(game, f.path))
		}
	}
	if len(hidden) > 0 {
		note += strings.Join(hidden, ", ") + " перекрыт" + yesNo(len(hidden) > 1, "ы", "") + " старшим " + gameRel(game, src.path) + " — клиент видит только его таблицу.\n"
	}
	res := func(kind int, text string) (lkResult, error) {
		return lkResult{kind, note + text}, nil
	}
	rows, _, err := chunkRows(dbc)
	if err != nil {
		return lkResult{}, fmt.Errorf("%s: %w", gameRel(game, src.path), err)
	}
	miss := len(lkMissing(rows))
	if miss == 0 {
		if mine == "" {
			return res(lkHave, "Позиции на Личе в порядке: строки платформы уже есть в "+gameRel(game, src.path)+".")
		}
		if gameRunning() {
			return lkResult{lkSkip, lkRunning}, nil
		}
		if err := os.Remove(mine); err != nil {
			return lkResult{}, fmt.Errorf("не смог удалить свой %s: %w", gameRel(game, mine), err)
		}
		forget()
		return res(lkPut, "Позиции на Личе в порядке: строки платформы уже есть в "+gameRel(game, src.path)+" — свой "+gameRel(game, mine)+" не нужен и удалён.")
	}
	if why := lkFloorCheck(game, others); why != "" {
		return res(lkSkip, "Не поставлен: "+why+".")
	}
	base := bytesSHA(dbc)
	above := mineAt > srcAt
	patched, err := lkBuild(dbc)
	if err != nil {
		return lkResult{}, err
	}
	want := writeMPQ(mpqEntry{lkChunk, patched})
	same := strings.EqualFold(mark.Base, src.path) && mark.BaseSHA == base
	if mine != "" && above && same && mark.SHA == bytesSHA(want) {
		return res(lkHave, "Позиции на Личе в порядке: всё на месте — свой "+gameRel(game, mine)+" поверх "+gameRel(game, src.path)+".")
	}
	why := ""
	switch {
	case mine == "":
	case !above:
		why = "Свой " + gameRel(game, mine) + " перекрыт новым " + gameRel(game, src.path) + " без строк платформы — удалён и собран заново выше: "
	case !strings.EqualFold(mark.Base, src.path):
		why = "Под своим " + gameRel(game, mine) + " появился " + gameRel(game, src.path) + " — свой пересобран поверх его таблицы, чтобы не затирать её: "
	case same:
		why = "Свой " + gameRel(game, mine) + " собран прежней версией обновлялки — пересобран: "
	default:
		why = "Таблица в " + gameRel(game, src.path) + " под своим " + gameRel(game, mine) + " сменилась — свой пересобран: "
	}
	name, top := "", ""
	if mine != "" && above && strings.EqualFold(filepath.Dir(mine), ldir) {
		name = filepath.Base(mine)
	} else {
		name, top = lkLetter(ldir, loc, filepath.Base(mine), others, with)
	}
	if name == "" {
		if top != "" {
			return res(lkSkip, "Не поставлен: после "+gameRel(game, top)+" свободной буквы нет — уберите или переименуйте этот архив.")
		}
		return res(lkSkip, "Не поставлен: все имена patch-"+loc+"-A…Z заняты.")
	}
	if gameRunning() {
		return lkResult{lkSkip, lkRunning}, nil
	}
	if mine != "" {
		if err := os.Remove(mine); err != nil {
			return lkResult{}, fmt.Errorf("не смог удалить свой %s: %w", gameRel(game, mine), err)
		}
		forget()
	}
	dst := filepath.Join(ldir, name)
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return lkResult{}, fmt.Errorf("не смог положить %s: %w", gameRel(game, dst), err)
	}
	if _, err = f.Write(want); err == nil {
		err = f.Close()
	} else {
		f.Close()
	}
	if err != nil {
		_ = os.Remove(dst)
		return lkResult{}, fmt.Errorf("не смог записать %s: %w", gameRel(game, dst), err)
	}
	*marks = append(*marks, lkMark{Path: dst, SHA: fileSHA(dst), Base: src.path, BaseSHA: base})
	done := fmt.Sprintf("%s поверх %s (%d → %d %s). Перезапустите игру.", gameRel(game, dst), gameRel(game, src.path), len(rows), len(rows)+miss, ruPlural(len(rows)+miss, "строка", "строки", "строк"))
	if why != "" {
		return res(lkPut, why+done)
	}
	return res(lkPut, "Поставлен "+done)
}

func bytesSHA(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func lkRun(addons string) (lkResult, error) {
	unlock, err := lockInstall(addons)
	if err != nil {
		return lkResult{lkSkip, "Другая копия обновлялки сейчас ставит аддон или патч — нажмите ещё раз через минуту."}, nil
	}
	defer unlock()
	return lkPatch(gameDir(addons))
}

func lkCLI(addons string) int {
	res, err := lkRun(addons)
	text, kind := lkText(res, err, false)
	autoLog("позиции на Личе: %s", strings.ReplaceAll(text, "\n", " "))
	fmt.Println(text)
	if kind == kindErr {
		return 1
	}
	return 0
}

func lkWanted(addons string) bool {
	d, ok := defByName(raidHelperName)
	return ok && addons != "" && d.at(addons).localVersion() != ""
}

func lkText(res lkResult, err error, auto bool) (string, int) {
	switch {
	case err != nil:
		return "Не вышло: " + err.Error(), kindErr
	case auto && res.text == lkRunning:
		return lkWait, kindNone
	case res.kind == lkSkip:
		return res.text, kindErr
	}
	return res.text, kindOK
}

func lkAuto(addons string) {
	if !lkWanted(addons) {
		return
	}
	res, err := lkRun(addons)
	text, _ := lkText(res, err, true)
	cfg := loadConfig()
	st := cfg.auto()
	st.note(noteLK, "позиции на Личе: "+strings.ReplaceAll(text, "\n", " "))
	storeAuto(st)
}

func (a *app) lkFix(auto bool) {
	a.mu.Lock()
	addons := a.addons
	if a.lkBusy || addons == "" || auto && !lkWanted(addons) {
		a.mu.Unlock()
		return
	}
	a.lkBusy, a.lkStatus, a.lkKind = true, "Смотрю архивы игры…", kindNone
	a.mu.Unlock()
	a.changed()
	go func() {
		res, err := lkRun(addons)
		text, kind := lkText(res, err, auto)
		autoLog("позиции на Личе%s: %s", yesNo(auto, " (сам)", ""), strings.ReplaceAll(text, "\n", " "))
		a.mu.Lock()
		a.lkBusy, a.lkStatus, a.lkKind = false, text, kind
		a.mu.Unlock()
		a.changed()
	}()
}

func (a *app) lkView() (bool, string, int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lkBusy, a.lkStatus, a.lkKind
}

const (
	lkFixBtn = "Починить позиции на Личе"
	lkWait   = "Игра запущена — проверю позиции на Личе, когда её закроют."
)
