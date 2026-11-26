package main

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const ownSV = "\r\nManaCodeRaidHelperDB = {\r\n\t[\"note\"] = \"закрывающая } и\\\r\nManaCodeRaidLeadDB = { в строке\",\r\n\t[\"seg\"] = {\r\n\t\t{\r\n\t\t\t[\"v\"] = 2,\r\n\t\t},\r\n\t},\r\n\t[\"q\"] = '[[не длинная]]',\r\n}\r\nManaCodeRaidLeadDB = {\r\n\t[\"lead\"] = \"мой сборщик\",\r\n}\r\n"

const setSV = "\nManaCodeRaidHelperDB = {\n\t[\"testSet\"] = {\n\t\t[\"id\"] = \"main-2709\",\n\t},\n\t[\"seg\"] = {\n\t\t[1] = \"набор\",\n\t},\n}\nManaCodeRaidLeadDB = {\n\t[\"lead\"] = \"чужой сборщик\",\n}\n"

func TestLuaTopLevel(t *testing.T) {
	stmts, ok := luaTopLevel([]byte(ownSV))
	if !ok || len(stmts) != 2 || stmts[0].name != rhDBVar || stmts[1].name != "ManaCodeRaidLeadDB" {
		t.Fatalf("разбор своего файла: %v %+v", ok, stmts)
	}
	lead := ownSV[stmts[1].start:stmts[1].end]
	if !strings.HasPrefix(lead, "ManaCodeRaidLeadDB = {") || !strings.Contains(lead, "мой сборщик") || strings.Contains(lead, "testSet") {
		t.Errorf("блок сборщика: %q", lead)
	}
	tricky := "A = [==[\n}\nB = 1\n]==]\n-- C = 2\n--[[\nD = 3\n]]\nE = { F = 1 }\nG == 1\n"
	stmts, ok = luaTopLevel([]byte(tricky))
	var names []string
	for _, s := range stmts {
		names = append(names, s.name)
	}
	if !ok || strings.Join(names, ",") != "A,E" {
		t.Errorf("длинные строки и комментарии: %v %v", ok, names)
	}
	if _, ok := luaTopLevel([]byte("A = {\n\t[\"x\"] = 1,\n")); ok {
		t.Error("оборванный файл должен не разобраться")
	}
}

func svPath(t *testing.T, root, acc string) string {
	t.Helper()
	p := filepath.Join(root, "WTF", "Account", acc, "SavedVariables", rhSVFile)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPlaceAndRestore(t *testing.T) {
	stubGame(t, false)
	root := t.TempDir()
	sv := svPath(t, root, "ACC")
	if err := os.WriteFile(sv, []byte(ownSV), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 29, 21, 14, 0, 0, time.Local)
	for i := 1; i <= 3; i++ {
		old := sv + mineSuffix + "2601" + string(rune('0'+i)) + "0-1200"
		_ = os.WriteFile(old, []byte("старая "+string(rune('0'+i))), 0o644)
	}
	warn, err := placeTestSet(sv, []byte(setSV), now)
	if err != nil || warn != "" {
		t.Fatalf("набор: %q %v", warn, err)
	}
	got, _ := os.ReadFile(sv)
	s := string(got)
	if !strings.Contains(s, "main-2709") || !strings.Contains(s, "мой сборщик") || strings.Contains(s, "чужой сборщик") || strings.Contains(s, "закрывающая") {
		t.Errorf("после набора: %q", s)
	}
	if stmts, ok := luaTopLevel(got); !ok || len(stmts) != 2 {
		t.Errorf("после набора файл не разбирается: %+v", stmts)
	}
	backup := sv + mineSuffix + "260929-2114"
	if b, err := os.ReadFile(backup); err != nil || string(b) != ownSV {
		t.Errorf("копия своей: %v", err)
	}
	if list := mineCopies(sv); len(list) != mineKeep || list[len(list)-1] != backup {
		t.Errorf("копий должно остаться %d: %v", mineKeep, list)
	}

	if _, err := placeTestSet(sv, []byte(setSV), now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if list := mineCopies(sv); list[len(list)-1] != backup {
		t.Errorf("тестовая запись не должна уходить в копию своей: %v", list)
	}

	edited := strings.Replace(string(got), "мой сборщик", "сборщик после набора", 1)
	_ = os.WriteFile(sv, []byte(edited), 0o644)
	used, err := restoreMine(sv)
	if err != nil || used != backup {
		t.Fatalf("вернуть свою: %q %v", used, err)
	}
	back, _ := os.ReadFile(sv)
	b := string(back)
	if !strings.Contains(b, "закрывающая") || strings.Contains(b, "main-2709") || !strings.Contains(b, "сборщик после набора") {
		t.Errorf("после возврата: %q", b)
	}
	if _, err := restoreMine(sv); err == nil {
		t.Error("своя стоит — второй возврат должен отказать")
	}
}

func TestPlaceEdgeCases(t *testing.T) {
	stubGame(t, false)
	root := t.TempDir()
	sv := svPath(t, root, "NEW")
	if _, err := placeTestSet(sv, []byte(setSV), time.Now()); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(sv)
	if strings.Contains(string(got), "ManaCodeRaidLeadDB") || !strings.Contains(string(got), "main-2709") {
		t.Errorf("без своего файла кладётся только набор: %q", got)
	}
	if len(mineCopies(sv)) != 0 {
		t.Error("копии без своего файла быть не должно")
	}
	if _, err := restoreMine(sv); err == nil {
		t.Error("копий нет — возврат должен отказать")
	}
	noMark := strings.Replace(setSV, `["testSet"]`, `["other"]`, 1)
	if _, err := placeTestSet(sv, []byte(noMark), time.Now()); err == nil {
		t.Error("набор без пометки ставить нельзя")
	}
	if _, err := placeTestSet(sv, []byte("SomethingElse = {}\n"), time.Now()); err == nil {
		t.Error("чужой файл ставить нельзя")
	}

	broken := svPath(t, root, "BROKEN")
	_ = os.WriteFile(broken, []byte("ManaCodeRaidHelperDB = {\n\t[\"x\"] = \"оборвано\n"), 0o644)
	warn, err := placeTestSet(broken, []byte(setSV), time.Now())
	if err != nil || warn == "" {
		t.Errorf("битый свой файл: %q %v", warn, err)
	}
	if len(mineCopies(broken)) != 1 {
		t.Error("битый свой файл должен уйти в копию")
	}
}

func aesZip(t *testing.T, name string, plain []byte, pass string) []byte {
	t.Helper()
	var def bytes.Buffer
	fw, _ := flate.NewWriter(&def, flate.BestSpeed)
	_, _ = fw.Write(plain)
	_ = fw.Close()
	salt := []byte("0123456789abcdef")
	k, err := pbkdf2.Key(sha1.New, pass, salt, 1000, 66)
	if err != nil {
		t.Fatal(err)
	}
	body := append([]byte(nil), def.Bytes()...)
	if err := winzipCTR(k[:32], body); err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha1.New, k[32:64])
	mac.Write(body)
	data := append(append(append(append([]byte(nil), salt...), k[64:66]...), body...), mac.Sum(nil)[:10]...)
	extra := []byte{0x01, 0x99, 7, 0, 2, 0, 'A', 'E', 3, 8, 0}
	var h bytes.Buffer
	for _, v := range []any{uint32(0x04034b50), uint16(51), uint16(1), uint16(99), uint16(0), uint16(0), uint32(0),
		uint32(len(data)), uint32(len(plain)), uint16(len(name)), uint16(len(extra))} {
		_ = binary.Write(&h, binary.LittleEndian, v)
	}
	h.WriteString(name)
	h.Write(extra)
	h.Write(data)
	return h.Bytes()
}

func plainZip(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create(name)
	_, _ = w.Write(body)
	_ = zw.Close()
	return buf.Bytes()
}

func TestOpenTestZip(t *testing.T) {
	enc := aesZip(t, rhSVFile, []byte(setSV), "raids-circle")
	if got, err := openTestZip(enc, "raids-circle"); err != nil || string(got) != setSV {
		t.Errorf("AES с паролем: %v", err)
	}
	if _, err := openTestZip(enc, "другой"); !errors.Is(err, errPassword) {
		t.Errorf("чужой пароль: %v", err)
	}
	if got, err := openTestZip(plainZip(t, rhSVFile, []byte(setSV)), ""); err != nil || string(got) != setSV {
		t.Errorf("zip без пароля: %v", err)
	}
	if _, err := openTestZip([]byte("<html>"), ""); err == nil {
		t.Error("не архив")
	}
	sum := sha256.Sum256(enc)
	if checkSHA(enc, strings.ToUpper(hex.EncodeToString(sum[:]))) != nil {
		t.Error("sha256 без учёта регистра")
	}
	if checkSHA(enc, strings.Repeat("0", 64)) == nil || checkSHA(enc, "") == nil {
		t.Error("sha256 не тот или пустой — отказ")
	}
}

func TestSetFit(t *testing.T) {
	s := testSet{Rec: 2, AddonMin: "0.2.0-beta.1"}
	cases := []struct {
		ver  string
		rec  int
		fits bool
	}{
		{"", 0, false},
		{"0.2.0-beta.1", 2, true},
		{"0.2.0", 2, true},
		{"0.1.0", 2, false},
		{"0.2.0-beta.2", 0, false},
		{"0.2.0-beta.2", 1, false},
		{"0.3.0", 3, false},
	}
	for _, c := range cases {
		if why := setFit(s, c.ver, c.rec); (why == "") != c.fits {
			t.Errorf("setFit(%q, %d) = %q", c.ver, c.rec, why)
		}
	}
}

func TestInstallTestSetHTTP(t *testing.T) {
	retryPause = 0
	pass := "raids-circle"
	oldPass := zipPass
	zipPass = pass
	t.Cleanup(func() { zipPass = oldPass })
	enc := aesZip(t, rhSVFile, []byte(setSV), pass)
	sum := sha256.Sum256(enc)
	idx := testIndex{V: 1, Baked: "2026-09-30T12:00:00Z", Sets: []testSet{{
		ID: "main-2709", Title: "Запись заказчика", Rec: 2, Addon: "0.2.0-beta.1", AddonMin: "0.2.0-beta.1",
		Attempts: 52, Bytes: int64(len(setSV)), Zip: "rec/main-2709.zip", ZipLen: int64(len(enc)), SHA256: hex.EncodeToString(sum[:]),
	}}}
	indexBody := func() []byte { b, _ := json.Marshal(idx); return b }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/data/index.json":
			_, _ = w.Write(indexBody())
		case "/data/rec/main-2709.zip":
			_, _ = w.Write(enc)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	oldData := raidHelperData
	raidHelperData = srv.URL + "/data/"
	t.Cleanup(func() { raidHelperData = oldData })

	got, err := fetchTestIndex()
	if err != nil || len(got.Sets) != 1 || got.Sets[0].ID != "main-2709" {
		t.Fatalf("index.json: %+v %v", got, err)
	}

	root, dir := layout(t)
	sv := svPath(t, root, "ACCOUNT2")
	_ = os.WriteFile(sv, []byte(ownSV), 0o644)
	accs := listAccounts(accountsDir(dir), loadConfig())
	if len(accs) != 1 || accs[0].name != "ACCOUNT2" || !accs[0].exists {
		t.Fatalf("аккаунты: %+v", accs)
	}

	stubGame(t, true)
	if _, err := installTestSet(dir, got.Sets[0], accs[0], &counter{}); !errors.Is(err, errGameRunning) {
		t.Errorf("игра запущена: %v", err)
	}
	stubGame(t, false)
	bad := got.Sets[0]
	bad.SHA256 = strings.Repeat("a", 64)
	if _, err := installTestSet(dir, bad, accs[0], &counter{}); err == nil {
		t.Error("sha256 не сошёлся — отказ")
	}
	if b, _ := os.ReadFile(sv); string(b) != ownSV {
		t.Error("при отказе своя запись тронута")
	}

	text, err := installTestSet(dir, got.Sets[0], accs[0], &counter{})
	if err != nil || !strings.Contains(text, "ACCOUNT2") {
		t.Fatalf("установка набора: %q %v", text, err)
	}
	if b, _ := os.ReadFile(sv); !strings.Contains(string(b), "main-2709") || !strings.Contains(string(b), "мой сборщик") {
		t.Errorf("набор не лёг: %q", b)
	}
	accs = listAccounts(accountsDir(dir), loadConfig())
	if accs[0].set != "main-2709" || accs[0].mines != 1 {
		t.Errorf("пометка набора: %+v", accs[0])
	}
	if text, err := returnMine(dir, accs[0]); err != nil || !strings.Contains(text, "вернулась") {
		t.Fatalf("вернуть свою: %q %v", text, err)
	}
	if b, _ := os.ReadFile(sv); !strings.Contains(string(b), "закрывающая") {
		t.Error("своя не вернулась")
	}
	if accs = listAccounts(accountsDir(dir), loadConfig()); accs[0].set != "" {
		t.Errorf("пометка не снялась: %+v", accs[0])
	}

	idx.V = 2
	if _, err := fetchTestIndex(); err == nil {
		t.Error("index.json новее обновлялки — отказ")
	}
	idx.V, idx.Sets = 1, nil
	if _, err := fetchTestIndex(); err == nil {
		t.Error("пустой список наборов — отказ")
	}
}
