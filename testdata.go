package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var raidHelperData = "https://raw.githubusercontent.com/dmitriinosach/-MANACODE-RaidHelper/data/"

const (
	testIndexV  = 1
	rhDBVar     = "ManaCodeRaidHelperDB"
	rhSVFile    = raidHelperName + ".lua"
	mineSuffix  = ".mine-"
	mineKeep    = 2
	testSetMark = `["testSet"]`
)

var reRecFormat = regexp.MustCompile(`(?m)^## X-RecFormat:\s*(\d+)`)

type testSet struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Rec      int    `json:"rec"`
	Scan     int    `json:"scan"`
	Totals   int    `json:"totals"`
	Trash    int    `json:"trash"`
	FX       int    `json:"fx"`
	Cls      int    `json:"cls"`
	Addon    string `json:"addon"`
	AddonMin string `json:"addonMin"`
	Attempts int    `json:"attempts"`
	Bytes    int64  `json:"bytes"`
	Zip      string `json:"zip"`
	ZipLen   int64  `json:"zipBytes"`
	SHA256   string `json:"sha256"`
}

type testIndex struct {
	V     int       `json:"v"`
	Baked string    `json:"baked"`
	Sets  []testSet `json:"sets"`
}

type testAccount struct {
	name   string
	sv     string
	exists bool
	size   int64
	mod    time.Time
	mines  int
	set    string
}

func (a otherAddon) recFormat() int {
	if m := reRecFormat.FindSubmatch(a.toc()); m != nil {
		v, _ := strconv.Atoi(string(m[1]))
		return v
	}
	return 0
}

func accountsDir(dir string) string {
	return filepath.Join(dir, "..", "..", "..", "WTF", "Account")
}

func fetchTestIndex() (*testIndex, error) {
	var body []byte
	var err error
	for try := 0; try < 3; try++ {
		if try > 0 {
			time.Sleep(retryPause)
		}
		if body, err = fetchRaw(raidHelperData+"index.json", "списка наборов", nil); err == nil {
			break
		}
	}
	if err != nil {
		return nil, err
	}
	var idx testIndex
	if err := json.Unmarshal(body, &idx); err != nil {
		return nil, fmt.Errorf("список наборов не читается: %w", err)
	}
	if idx.V > testIndexV {
		return nil, fmt.Errorf("список наборов новее этой обновлялки — возьмите свежую ОбновитьДанные.exe")
	}
	if len(idx.Sets) == 0 {
		return nil, fmt.Errorf("тестовых записей пока нет")
	}
	return &idx, nil
}

func setFit(s testSet, ver string, rec int) string {
	switch {
	case ver == "":
		return "сначала поставьте Raid Helper: Esc, затем G"
	case s.AddonMin != "" && newer(s.AddonMin, ver):
		return "нужен Raid Helper " + s.AddonMin + " или новее"
	case rec == 0 || s.Rec > rec:
		return "запись новее вашего Raid Helper — обновите его"
	case s.Rec < rec:
		return "запись старого формата — ждите новую"
	}
	return ""
}

func listAccounts(root string, cfg config) []testAccount {
	entries, _ := os.ReadDir(root)
	var out []testAccount
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		a := testAccount{name: e.Name(), sv: filepath.Join(root, e.Name(), "SavedVariables", rhSVFile)}
		if st, err := os.Stat(a.sv); err == nil {
			a.exists, a.size, a.mod = true, st.Size(), st.ModTime()
		}
		a.mines = len(mineCopies(a.sv))
		if cfg.RaidHelper != nil {
			a.set = cfg.RaidHelper.Sets[a.name]
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].name) < strings.ToLower(out[j].name) })
	return out
}

func mineCopies(sv string) []string {
	files, _ := filepath.Glob(sv + mineSuffix + "*")
	var out []string
	for _, f := range files {
		if !strings.HasSuffix(f, ".tmp") {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

func pruneMine(sv string, keep int) {
	list := mineCopies(sv)
	for len(list) > keep {
		_ = os.Remove(list[0])
		list = list[1:]
	}
}

type luaStmt struct {
	name       string
	start, end int
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdent(c byte) bool { return isIdentStart(c) || (c >= '0' && c <= '9') }

func longOpen(src []byte, i int) (int, bool) {
	if i >= len(src) || src[i] != '[' {
		return 0, false
	}
	j := i + 1
	for j < len(src) && src[j] == '=' {
		j++
	}
	if j < len(src) && src[j] == '[' {
		return j - i - 1, true
	}
	return 0, false
}

func longClose(src []byte, from, lvl int) int {
	closer := append(append([]byte{']'}, bytes.Repeat([]byte{'='}, lvl)...), ']')
	k := bytes.Index(src[from:], closer)
	if k < 0 {
		return -1
	}
	return from + k + len(closer)
}

func luaTopLevel(src []byte) ([]luaStmt, bool) {
	var out []luaStmt
	depth, n, i := 0, len(src), 0
	for i < n {
		c := src[i]
		switch {
		case c == '"' || c == '\'':
			i++
			for i < n && src[i] != c {
				switch src[i] {
				case '\\':
					i++
					if i+1 < n && src[i] == '\r' && src[i+1] == '\n' {
						i++
					}
				case '\n':
					return out, false
				}
				i++
			}
			if i >= n {
				return out, false
			}
		case c == '-' && i+1 < n && src[i+1] == '-':
			i += 2
			if lvl, ok := longOpen(src, i); ok {
				if i = longClose(src, i+lvl+2, lvl); i < 0 {
					return out, false
				}
				continue
			}
			for i < n && src[i] != '\n' {
				i++
			}
			continue
		case c == '[':
			if lvl, ok := longOpen(src, i); ok {
				if i = longClose(src, i+lvl+2, lvl); i < 0 {
					return out, false
				}
				continue
			}
		case c == '{':
			depth++
		case c == '}':
			if depth--; depth < 0 {
				return out, false
			}
		case depth == 0 && (i == 0 || src[i-1] == '\n') && isIdentStart(c):
			j := i
			for j < n && isIdent(src[j]) {
				j++
			}
			k := j
			for k < n && (src[k] == ' ' || src[k] == '\t') {
				k++
			}
			if k < n && src[k] == '=' && (k+1 >= n || src[k+1] != '=') {
				if len(out) > 0 {
					out[len(out)-1].end = i
				}
				out = append(out, luaStmt{name: string(src[i:j]), start: i})
			}
			i = j
			continue
		}
		i++
	}
	if len(out) > 0 {
		out[len(out)-1].end = n
	}
	return out, depth == 0
}

func stmtBlock(src []byte, stmts []luaStmt, name string) []byte {
	for _, s := range stmts {
		if s.name == name {
			b := src[s.start:s.end]
			if !bytes.HasSuffix(b, []byte("\n")) {
				b = append(append([]byte(nil), b...), '\n')
			}
			return b
		}
	}
	return nil
}

func replaceTop(src []byte, stmts []luaStmt, name string, block []byte) []byte {
	for _, s := range stmts {
		if s.name == name {
			out := make([]byte, 0, len(src)-(s.end-s.start)+len(block))
			out = append(out, src[:s.start]...)
			out = append(out, block...)
			return append(out, src[s.end:]...)
		}
	}
	out := append([]byte(nil), src...)
	if len(out) > 0 && !bytes.HasSuffix(out, []byte("\n")) {
		out = append(out, '\n')
	}
	return append(out, block...)
}

func isTestSet(b []byte) bool { return bytes.Contains(b, []byte(testSetMark)) }

func openTestZip(data []byte, pass string) ([]byte, error) {
	if len(data) >= 30 && binary.LittleEndian.Uint32(data) == 0x04034b50 && binary.LittleEndian.Uint16(data[8:]) == 99 {
		_, plain, err := unzipFirstAES(data, pass)
		return plain, err
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, errors.New("скачался не архив")
	}
	for _, f := range zr.File {
		if strings.HasSuffix(strings.ToLower(f.Name), ".lua") {
			rc, err := f.Open()
			if err != nil {
				return nil, errors.New("архив не распаковался")
			}
			b, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, errors.New("архив не распаковался")
			}
			return b, nil
		}
	}
	return nil, errors.New("в архиве нет файла записи")
}

func checkSHA(data []byte, want string) error {
	if want == "" {
		return errors.New("у набора нет контрольной суммы — не ставлю")
	}
	sum := sha256.Sum256(data)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), strings.TrimSpace(want)) {
		return errors.New("набор скачался с ошибкой (не сошлась контрольная сумма) — попробуйте ещё раз")
	}
	return nil
}

func placeTestSet(sv string, set []byte, now time.Time) (string, error) {
	setStmts, ok := luaTopLevel(set)
	block := stmtBlock(set, setStmts, rhDBVar)
	if !ok || block == nil {
		return "", errors.New("в наборе не тот файл")
	}
	if !isTestSet(block) {
		return "", errors.New("в наборе нет пометки тестовой записи — не ставлю")
	}
	own, err := os.ReadFile(sv)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	exists := err == nil
	out := append([]byte("\n"), block...)
	warn := ""
	if exists {
		if ownStmts, ok := luaTopLevel(own); ok {
			out = replaceTop(own, ownStmts, rhDBVar, block)
		} else {
			warn = "свой файл не разобрался — сборщик остался только в копии своей записи"
		}
	}
	backup := ""
	if exists && !isTestSet(own) {
		backup = sv + mineSuffix + now.Format("060102-1504")
		_ = os.Remove(backup)
		if err := os.Rename(sv, backup); err != nil {
			return "", fmt.Errorf("%s занят другой программой — закройте игру и повторите", filepath.Base(sv))
		}
	}
	if err := writeFile(sv, out); err != nil {
		if backup != "" {
			_ = os.Rename(backup, sv)
		}
		return "", err
	}
	pruneMine(sv, mineKeep)
	return warn, nil
}

func restoreMine(sv string) (string, error) {
	list := mineCopies(sv)
	if len(list) == 0 {
		return "", errors.New("копии своей записи нет — возвращать нечего")
	}
	last := list[len(list)-1]
	back, err := os.ReadFile(last)
	if err != nil {
		return "", err
	}
	cur, err := os.ReadFile(sv)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	out := back
	if err == nil {
		if !isTestSet(cur) {
			return "", errors.New("сейчас стоит своя запись, а не тестовая — возвращать нечего")
		}
		curStmts, ok1 := luaTopLevel(cur)
		backStmts, ok2 := luaTopLevel(back)
		if b := stmtBlock(back, backStmts, rhDBVar); ok1 && ok2 && b != nil {
			out = replaceTop(cur, curStmts, rhDBVar, b)
		}
	}
	if err := gameClosed(); err != nil {
		return "", err
	}
	if err := writeFile(sv, out); err != nil {
		return "", err
	}
	return last, nil
}

func markTestSet(dir, account, id string) {
	cfg := loadConfig()
	rh := cfg.rh()
	if rh.Sets == nil {
		rh.Sets = map[string]string{}
	}
	if id == "" {
		delete(rh.Sets, account)
	} else {
		rh.Sets[account] = id
	}
	saveConfig(cfg)
}

func installTestSet(dir string, set testSet, acc testAccount, cnt *counter) (string, error) {
	if err := gameClosed(); err != nil {
		return "", err
	}
	cnt.file.Store("скачиваю")
	cnt.got.Store(0)
	cnt.total.Store(set.ZipLen)
	data, err := fetchRaw(raidHelperData+set.Zip, "набора", cnt)
	if err != nil {
		return "", err
	}
	cnt.file.Store("проверяю и распаковываю")
	if err := checkSHA(data, set.SHA256); err != nil {
		return "", err
	}
	content, err := openTestZip(data, zipPass)
	if err != nil {
		return "", err
	}
	data = nil
	cnt.file.Store("кладу запись")
	if err := gameClosed(); err != nil {
		return "", err
	}
	warn, err := placeTestSet(acc.sv, content, time.Now())
	if err != nil {
		return "", err
	}
	markTestSet(dir, acc.name, set.ID)
	text := "Тестовая запись стоит в аккаунте " + acc.name + "."
	if warn != "" {
		text += " " + warn
	}
	return text, nil
}

func returnMine(dir string, acc testAccount) (string, error) {
	if err := gameClosed(); err != nil {
		return "", err
	}
	last, err := restoreMine(acc.sv)
	if err != nil {
		return "", err
	}
	markTestSet(dir, acc.name, "")
	stamp := strings.TrimPrefix(filepath.Base(last), filepath.Base(acc.sv)+mineSuffix)
	if t, err := time.ParseInLocation("060102-1504", stamp, time.Local); err == nil {
		stamp = t.Format("02.01 15:04")
	}
	return "Своя запись в аккаунте " + acc.name + " вернулась (копия от " + stamp + ").", nil
}
