package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func withRows(dbc []byte, rows ...[4]uint32) []byte {
	le := binary.LittleEndian
	recs := le.Uint32(dbc[4:])
	out := append([]byte{}, dbc[:20+recs*20]...)
	for _, r := range rows {
		for _, v := range r {
			out = le.AppendUint32(out, v)
		}
		out = le.AppendUint32(out, 0)
	}
	le.PutUint32(out[4:], recs+uint32(len(rows)))
	return append(out, dbc[20+recs*20:]...)
}

func dmapDBC(floors ...[2]uint32) []byte {
	le := binary.LittleEndian
	out := []byte("WDBC")
	for _, v := range []uint32{uint32(len(floors)), 8, 32, 1} {
		out = le.AppendUint32(out, v)
	}
	for _, f := range floors {
		for _, v := range []uint32{f[0], f[1], 1, 0, 0, 0, 0, 0} {
			out = le.AppendUint32(out, v)
		}
	}
	return append(out, 0)
}

func etalon(t *testing.T) (asset, stock []byte) {
	t.Helper()
	asset, err := os.ReadFile(filepath.Join("testdata", lkOld))
	if err != nil {
		t.Fatal(err)
	}
	arc, err := readMPQ(bytes.NewReader(asset), int64(len(asset)))
	if err != nil {
		t.Fatal(err)
	}
	dbc, err := arc.read(lkChunk)
	if err != nil {
		t.Fatal(err)
	}
	rows, strs, err := chunkRows(dbc)
	if err != nil {
		t.Fatal(err)
	}
	stock = append([]byte{}, dbc[:20]...)
	n := 0
	for _, r := range rows {
		if len(lkMissing([][]byte{r})) == len(lkGroups) {
			stock = append(stock, r...)
			n++
		}
	}
	binary.LittleEndian.PutUint32(stock[4:], uint32(n))
	return asset, append(stock, strs...)
}

func lkClient(t *testing.T, files map[string][]byte) string {
	t.Helper()
	game := t.TempDir()
	put(t, filepath.Join(game, "WTF", "Config.wtf"), "SET locale \"ruRU\"\r\n")
	if _, ok := files["ruRU/locale-ruRU.MPQ"]; !ok {
		files["ruRU/locale-ruRU.MPQ"] = writeMPQ(mpqEntry{lkDMap, dmapDBC([2]uint32{108, 631}, [2]uint32{109, 631}, [2]uint32{110, 600})})
	}
	for rel, b := range files {
		p := filepath.Join(game, "Data", filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return game
}

func chunkMPQ(dbc []byte) []byte {
	return writeMPQ(mpqEntry{lkChunk, dbc})
}

func otherMPQ() []byte {
	return writeMPQ(mpqEntry{`Textures\floor.blp`, []byte("BLP2")})
}

func gameFiles(t *testing.T, game string) map[string]string {
	t.Helper()
	out := map[string]string{}
	_ = filepath.Walk(filepath.Join(game, "Data"), func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			b, _ := os.ReadFile(p)
			out[gameRel(game, p)] = string(b)
		}
		return nil
	})
	return out
}

func activeChunk(t *testing.T, game string) (string, []byte) {
	t.Helper()
	src, dbc, _ := lkLast(mpqLoad(filepath.Join(game, "Data"), "ruRU"), lkChunk)
	return filepath.Base(src.path), dbc
}

func locPath(game, name string) string {
	return filepath.Join(game, "Data", "ruRU", name)
}

func TestLKWriterMatchesRelease(t *testing.T) {
	asset, stock := etalon(t)
	rows, _, err := chunkRows(stock)
	if err != nil || len(rows) != 622 {
		t.Fatalf("сток — 622 строки: %d %v", len(rows), err)
	}
	arc, _ := readMPQ(bytes.NewReader(asset), int64(len(asset)))
	tail, _ := arc.read(lkChunk)
	if got := writeMPQ(mpqEntry{lkChunk, tail}); !bytes.Equal(got, asset) {
		t.Fatalf("писатель MPQ — байт в байт как lkmap_patch.js (%d против %d байт)", len(got), len(asset))
	}
	patched, err := lkBuild(stock)
	if err != nil {
		t.Fatal(err)
	}
	if at, errs := lkPlace(patched, stock); len(errs) > 0 || !reflect.DeepEqual(at, []int{599, 600}) {
		t.Errorf("в стоке новые строки — 599 и 600 из 624: %v %v", at, errs)
	}
	if _, errs := lkPlace(tail, stock); len(errs) == 0 {
		t.Error("строки в хвосте (patch-A.MPQ 21.10) проверка мест ловит")
	}
	if !lkOldOurs(filepath.Join("testdata", lkOld)) {
		t.Error("patch-A.MPQ релиза узнаётся как наш")
	}
	other := filepath.Join(t.TempDir(), lkOld)
	_ = os.WriteFile(other, chunkMPQ(stock), 0o644)
	if lkOldOurs(other) {
		t.Error("чужой patch-A.MPQ — не наш")
	}
}

func lkPlace(got, base []byte) ([]int, []string) {
	rows, strs, err := chunkRows(got)
	if err != nil {
		return nil, []string{err.Error()}
	}
	le := binary.LittleEndian
	var at []int
	var errs []string
	var kept []byte
	for i, r := range rows {
		m, g := le.Uint32(r[4:]), le.Uint32(r[8:])
		if m != lkMap || g != lkGroups[0] && g != lkGroups[1] {
			kept = append(kept, r...)
			continue
		}
		at = append(at, i)
		if i == 0 || le.Uint32(rows[i-1][4:]) != lkMap || le.Uint32(rows[i-1][8:]) >= g {
			errs = append(errs, fmt.Sprintf("группа %d: слева не карта 631 с меньшей группой", g))
		}
		if i+1 < len(rows) && le.Uint32(rows[i+1][4:]) == lkMap && le.Uint32(rows[i+1][8:]) <= g {
			errs = append(errs, fmt.Sprintf("группа %d: справа карта 631 с группой не больше", g))
		}
	}
	brows, bstrs, _ := chunkRows(base)
	if len(at) != len(lkGroups) || len(rows) != len(brows)+len(lkGroups) || !bytes.Equal(kept, base[20:20+len(brows)*20]) || !bytes.Equal(strs, bstrs) {
		errs = append(errs, fmt.Sprintf("исходные записи и блок строк не в прежнем порядке: новых %d", len(at)))
	}
	return at, errs
}

func placed(t *testing.T, got, base []byte) {
	t.Helper()
	if _, errs := lkPlace(got, base); len(errs) > 0 {
		t.Errorf("места строк: %v", errs)
	}
}

func dataRel(parts ...string) string {
	return filepath.Join(append([]string{"Data"}, parts...)...)
}

func TestLKStockGetsLocalA(t *testing.T) {
	stubGame(t, false)
	stubUserConfig(t)
	asset, stock := etalon(t)
	files := map[string][]byte{
		"patch-3.MPQ":           otherMPQ(),
		"patch-A.MPQ":           asset,
		"patch-P.MPQ":           chunkMPQ(withRows(stock, [4]uint32{5000, 631, 28150, 109}, [4]uint32{5001, 631, 28151, 109})),
		"ruRU/patch-ruRU-3.MPQ": chunkMPQ(stock),
		"ruRU/patch-ruRU-2.MPQ": chunkMPQ(stock[:20]),
		"ruRU/backup-ruRU.MPQ":  chunkMPQ(stock[:20]),
		"ruRU/patch-ruRU-x.txt": []byte("x"),
	}
	for _, l := range []string{"B", "D", "H", "V", "Z"} {
		files["ruRU/patch-ruRU-"+l+".MPQ"] = otherMPQ()
	}
	files["ruRU/patch-ruRU-y.mpq"] = otherMPQ()
	game := lkClient(t, files)
	res, err := lkPatch(game)
	hidden := dataRel("patch-P.MPQ") + " перекрыт старшим " + dataRel("ruRU", "patch-ruRU-3.MPQ") + " — клиент видит только его таблицу.\n"
	want := "Удалён старый " + dataRel("patch-A.MPQ") + " из прошлого релиза.\n" + hidden + "Поставлен " + dataRel("ruRU", "patch-ruRU-A.MPQ") + " поверх " + dataRel("ruRU", "patch-ruRU-3.MPQ") + " (622 → 624 строки). Перезапустите игру."
	if err != nil || res.kind != lkPut || res.text != want {
		t.Fatalf("сток — patch-ruRU-A, старый общий удалён, общий P перекрыт:\n%q\n%v", res.text, err)
	}
	if exists(filepath.Join(game, "Data", "patch-A.MPQ")) {
		t.Error("старый общий patch-A.MPQ удалён")
	}
	if name, dbc := activeChunk(t, game); name != "patch-ruRU-A.MPQ" {
		t.Errorf("действует наш: %s", name)
	} else {
		placed(t, dbc, stock)
	}
	if m := loadLKMarks(); len(m) != 1 || m[0].Path != locPath(game, "patch-ruRU-A.MPQ") || m[0].SHA != fileSHA(m[0].Path) || m[0].Base != locPath(game, "patch-ruRU-3.MPQ") || m[0].BaseSHA != bytesSHA(stock) {
		t.Errorf("своя запись: имя, хэш, основа и хэш её таблицы: %+v", m)
	}
	before := gameFiles(t, game)
	stubGame(t, true)
	res, err = lkPatch(game)
	if err != nil || res.kind != lkHave || res.text != hidden+"Позиции на Личе в порядке: всё на месте — свой "+dataRel("ruRU", "patch-ruRU-A.MPQ")+" поверх "+dataRel("ruRU", "patch-ruRU-3.MPQ")+"." {
		t.Fatalf("повтор — всё на месте, игра может идти: %q %v", res.text, err)
	}
	if !reflect.DeepEqual(before, gameFiles(t, game)) {
		t.Error("повтор ничего не меняет")
	}
}

func TestLKForeignWithRows(t *testing.T) {
	stubGame(t, true)
	stubUserConfig(t)
	_, stock := etalon(t)
	patched := withRows(stock, [4]uint32{1830, 631, 28150, 109}, [4]uint32{1831, 631, 28151, 109})
	game := lkClient(t, map[string][]byte{
		"ruRU/patch-ruRU-3.MPQ": chunkMPQ(stock),
		"ruRU/patch-ruRU-M.MPQ": chunkMPQ(patched),
		"ruRU/patch-ruRU-Z.MPQ": otherMPQ(),
	})
	before := gameFiles(t, game)
	res, err := lkPatch(game)
	if err != nil || res.kind != lkHave || res.text != "Позиции на Личе в порядке: строки платформы уже есть в "+dataRel("ruRU", "patch-ruRU-M.MPQ")+"." {
		t.Fatalf("чужой со строками — не нужен: %q %v", res.text, err)
	}
	if !reflect.DeepEqual(before, gameFiles(t, game)) {
		t.Error("ничего не тронуто")
	}
}

func TestLKForeignWithoutRows(t *testing.T) {
	stubGame(t, false)
	stubUserConfig(t)
	_, stock := etalon(t)
	extra := make([][4]uint32, 1310)
	for i := range extra {
		extra[i] = [4]uint32{uint32(3000 + i), 600, uint32(i), 1}
	}
	foreign := withRows(stock, extra...)
	game := lkClient(t, map[string][]byte{
		"patch-3.MPQ":           chunkMPQ(stock),
		"ruRU/patch-ruRU-3.MPQ": chunkMPQ(stock),
		"ruRU/patch-ruRU-M.MPQ": chunkMPQ(foreign),
	})
	res, err := lkPatch(game)
	if err != nil || res.kind != lkPut || res.text != "Поставлен "+dataRel("ruRU", "patch-ruRU-N.MPQ")+" поверх "+dataRel("ruRU", "patch-ruRU-M.MPQ")+" (1932 → 1934 строки). Перезапустите игру." {
		t.Fatalf("чужой M без строк — N поверх M: %q %v", res.text, err)
	}
	name, dbc := activeChunk(t, game)
	rows, _, _ := chunkRows(dbc)
	if name != "patch-ruRU-N.MPQ" || len(rows) != 1934 {
		t.Fatalf("N поверх M: %s %d", name, len(rows))
	}
	placed(t, dbc, foreign)
	le := binary.LittleEndian
	at, _ := lkPlace(dbc, foreign)
	for i, k := range at {
		if r := rows[k]; le.Uint32(r) != 3000+1310+uint32(i) || le.Uint32(r[4:]) != 631 || le.Uint32(r[8:]) != lkGroups[i] || le.Uint32(r[12:]) != 109 || le.Uint32(r[16:]) != 0 {
			t.Errorf("строка %d: %v", i, r)
		}
	}
	if got, _ := os.ReadFile(locPath(game, "patch-ruRU-M.MPQ")); !bytes.Equal(got, chunkMPQ(foreign)) {
		t.Error("чужой M не тронут")
	}

	_ = os.Remove(locPath(game, "patch-ruRU-N.MPQ"))
	saveLKMarks(nil)
	put(t, locPath(game, "patch-ruRU-N.MPQ"), string(otherMPQ()))
	if res, err := lkPatch(game); err != nil || !strings.HasPrefix(res.text, "Поставлен "+dataRel("ruRU", "patch-ruRU-O.MPQ")) {
		t.Fatalf("N занята чужим без DBC — O: %q %v", res.text, err)
	}
}

func TestLKTwoForeign(t *testing.T) {
	stubGame(t, false)
	stubUserConfig(t)
	_, stock := etalon(t)
	low := withRows(stock, [4]uint32{4000, 600, 1, 1})
	high := withRows(stock, [4]uint32{4000, 600, 2, 2}, [4]uint32{4001, 600, 3, 3})
	game := lkClient(t, map[string][]byte{
		"ruRU/patch-ruRU-3.MPQ": chunkMPQ(stock),
		"ruRU/patch-ruRU-M.MPQ": chunkMPQ(low),
		"ruRU/patch-ruRU-P.MPQ": chunkMPQ(high),
	})
	res, err := lkPatch(game)
	want := dataRel("ruRU", "patch-ruRU-M.MPQ") + " перекрыт старшим " + dataRel("ruRU", "patch-ruRU-P.MPQ") + " — клиент видит только его таблицу.\nПоставлен " + dataRel("ruRU", "patch-ruRU-Q.MPQ") + " поверх " + dataRel("ruRU", "patch-ruRU-P.MPQ") + " (624 → 626 строк). Перезапустите игру."
	if err != nil || res.kind != lkPut || res.text != want {
		t.Fatalf("два чужих — поверх старшего, младший перекрыт:\n%q\n%v", res.text, err)
	}
	_, dbc := activeChunk(t, game)
	placed(t, dbc, high)
}

func TestLKRebuild(t *testing.T) {
	stubGame(t, false)
	stubUserConfig(t)
	_, stock := etalon(t)
	game := lkClient(t, map[string][]byte{"ruRU/patch-ruRU-3.MPQ": chunkMPQ(stock)})
	if res, err := lkPatch(game); err != nil || res.kind != lkPut || !exists(locPath(game, "patch-ruRU-A.MPQ")) {
		t.Fatalf("сток — A: %+v %v", res, err)
	}
	foreign := withRows(stock, [4]uint32{4000, 600, 1, 1})
	put(t, locPath(game, "patch-ruRU-M.MPQ"), string(chunkMPQ(foreign)))
	stubGame(t, true)
	if res, err := lkPatch(game); err != nil || res.kind != lkSkip || res.text != lkRunning || !exists(locPath(game, "patch-ruRU-A.MPQ")) || exists(locPath(game, "patch-ruRU-N.MPQ")) {
		t.Fatalf("игра запущена — закройте игру, ничего не тронуто: %q %v", res.text, err)
	}
	stubGame(t, false)
	res, err := lkPatch(game)
	want := "Свой " + dataRel("ruRU", "patch-ruRU-A.MPQ") + " перекрыт новым " + dataRel("ruRU", "patch-ruRU-M.MPQ") + " без строк платформы — удалён и собран заново выше: " + dataRel("ruRU", "patch-ruRU-N.MPQ") + " поверх " + dataRel("ruRU", "patch-ruRU-M.MPQ") + " (623 → 625 строк). Перезапустите игру."
	if err != nil || res.kind != lkPut || res.text != want {
		t.Fatalf("чужой M выше своего без строк — свой A удалён, N поверх M:\n%q\n%v", res.text, err)
	}
	if exists(locPath(game, "patch-ruRU-A.MPQ")) {
		t.Error("свой A удалён")
	}
	if name, dbc := activeChunk(t, game); name != "patch-ruRU-N.MPQ" {
		t.Errorf("действует N: %s", name)
	} else {
		placed(t, dbc, foreign)
	}
	if m := loadLKMarks(); len(m) != 1 || filepath.Base(m[0].Path) != "patch-ruRU-N.MPQ" || m[0].Base != locPath(game, "patch-ruRU-M.MPQ") {
		t.Errorf("запись переехала на N поверх M: %+v", m)
	}

	put(t, locPath(game, "patch-ruRU-M.MPQ"), string(chunkMPQ(withRows(foreign, [4]uint32{4001, 631, 28150, 109}, [4]uint32{4002, 631, 28151, 109}))))
	res, err = lkPatch(game)
	if err != nil || res.kind != lkPut || res.text != "Позиции на Личе в порядке: строки платформы уже есть в "+dataRel("ruRU", "patch-ruRU-M.MPQ")+" — свой "+dataRel("ruRU", "patch-ruRU-N.MPQ")+" не нужен и удалён." || exists(locPath(game, "patch-ruRU-N.MPQ")) {
		t.Fatalf("строки появились у M — свой N удалён: %q %v", res.text, err)
	}
	if res, err := lkPatch(game); err != nil || res.kind != lkHave || len(loadLKMarks()) != 0 {
		t.Fatalf("потом — в порядке по M: %+v %v", res, err)
	}

	game = lkClient(t, map[string][]byte{"ruRU/patch-ruRU-3.MPQ": chunkMPQ(stock)})
	_, _ = lkPatch(game)
	put(t, locPath(game, "patch-ruRU-Q.MPQ"), string(chunkMPQ(withRows(stock, [4]uint32{1828, 631, 28150, 109}, [4]uint32{1829, 631, 28151, 109}))))
	if res, err := lkPatch(game); err != nil || res.text != "Позиции на Личе в порядке: строки платформы уже есть в "+dataRel("ruRU", "patch-ruRU-Q.MPQ")+" — свой "+dataRel("ruRU", "patch-ruRU-A.MPQ")+" не нужен и удалён." || exists(locPath(game, "patch-ruRU-A.MPQ")) {
		t.Fatalf("чужой выше своего со строками — свой удалён: %q %v", res.text, err)
	}
}

func TestLKForeignBelow(t *testing.T) {
	stubGame(t, false)
	stubUserConfig(t)
	_, stock := etalon(t)
	game := lkClient(t, map[string][]byte{
		"ruRU/patch-ruRU-3.MPQ": chunkMPQ(stock),
		"ruRU/patch-ruRU-A.MPQ": otherMPQ(),
		"ruRU/patch-ruRU-B.MPQ": otherMPQ(),
		"ruRU/patch-ruRU-C.MPQ": otherMPQ(),
	})
	if res, err := lkPatch(game); err != nil || !strings.HasPrefix(res.text, "Поставлен "+dataRel("ruRU", "patch-ruRU-D.MPQ")) {
		t.Fatalf("D: %q %v", res.text, err)
	}
	foreign := withRows(stock, [4]uint32{4000, 600, 1, 1})
	put(t, locPath(game, "patch-ruRU-B.MPQ"), string(chunkMPQ(foreign)))
	res, err := lkPatch(game)
	want := "Под своим " + dataRel("ruRU", "patch-ruRU-D.MPQ") + " появился " + dataRel("ruRU", "patch-ruRU-B.MPQ") + " — свой пересобран поверх его таблицы, чтобы не затирать её: " + dataRel("ruRU", "patch-ruRU-D.MPQ") + " поверх " + dataRel("ruRU", "patch-ruRU-B.MPQ") + " (623 → 625 строк). Перезапустите игру."
	if err != nil || res.kind != lkPut || res.text != want {
		t.Fatalf("чужой B ниже своего D — D пересобран поверх B на той же букве:\n%q\n%v", res.text, err)
	}
	if name, dbc := activeChunk(t, game); name != "patch-ruRU-D.MPQ" {
		t.Errorf("действует D: %s", name)
	} else {
		placed(t, dbc, foreign)
	}

	put(t, locPath(game, "patch-ruRU-B.MPQ"), string(chunkMPQ(withRows(foreign, [4]uint32{4001, 600, 2, 2}))))
	res, err = lkPatch(game)
	if err != nil || !strings.HasPrefix(res.text, "Таблица в "+dataRel("ruRU", "patch-ruRU-B.MPQ")+" под своим "+dataRel("ruRU", "patch-ruRU-D.MPQ")+" сменилась — свой пересобран: "+dataRel("ruRU", "patch-ruRU-D.MPQ")+" поверх "+dataRel("ruRU", "patch-ruRU-B.MPQ")+" (624 → 626 строк)") {
		t.Fatalf("таблица основы сменилась на месте — пересобран: %q %v", res.text, err)
	}
	if res, err := lkPatch(game); err != nil || res.kind != lkHave || !strings.Contains(res.text, "всё на месте") {
		t.Fatalf("после пересборки — всё на месте: %q %v", res.text, err)
	}
}

func TestLKOwnLetterReused(t *testing.T) {
	stubGame(t, false)
	stubUserConfig(t)
	_, stock := etalon(t)
	files := map[string][]byte{"ruRU/patch-ruRU-3.MPQ": chunkMPQ(stock)}
	for c := 'B'; c <= 'Z'; c++ {
		files["ruRU/patch-ruRU-"+string(c)+".MPQ"] = otherMPQ()
	}
	game := lkClient(t, files)
	if res, err := lkPatch(game); err != nil || !strings.HasPrefix(res.text, "Поставлен "+dataRel("ruRU", "patch-ruRU-A.MPQ")) {
		t.Fatalf("A: %q %v", res.text, err)
	}
	put(t, locPath(game, "patch-ruRU-2.MPQ"), string(chunkMPQ(withRows(stock, [4]uint32{4000, 600, 1, 1}))))
	put(t, locPath(game, "patch-ruRU-3.MPQ"), string(otherMPQ()))
	res, err := lkPatch(game)
	if err != nil || !strings.HasPrefix(res.text, "Под своим "+dataRel("ruRU", "patch-ruRU-A.MPQ")+" появился "+dataRel("ruRU", "patch-ruRU-2.MPQ")) || !strings.Contains(res.text, ": "+dataRel("ruRU", "patch-ruRU-A.MPQ")+" поверх ") {
		t.Fatalf("основа сменилась, все буквы заняты — свой на той же букве: %q %v", res.text, err)
	}
}

func TestLKOnlyOwnFile(t *testing.T) {
	stubGame(t, false)
	stubUserConfig(t)
	_, stock := etalon(t)
	game := lkClient(t, map[string][]byte{"ruRU/patch-ruRU-3.MPQ": chunkMPQ(stock)})
	if _, err := lkPatch(game); err != nil {
		t.Fatal(err)
	}
	edited := chunkMPQ(withRows(stock, [4]uint32{9000, 600, 1, 1}))
	put(t, locPath(game, "patch-ruRU-A.MPQ"), string(edited))
	res, err := lkPatch(game)
	want := dataRel("ruRU", "patch-ruRU-A.MPQ") + " изменён не нами — не трогали.\nПоставлен " + dataRel("ruRU", "patch-ruRU-B.MPQ") + " поверх " + dataRel("ruRU", "patch-ruRU-A.MPQ") + " (623 → 625 строк). Перезапустите игру."
	if err != nil || res.kind != lkPut || res.text != want {
		t.Fatalf("наш A кто-то переписал — он чужой, B поверх:\n%q\n%v", res.text, err)
	}
	if got, _ := os.ReadFile(locPath(game, "patch-ruRU-A.MPQ")); !bytes.Equal(got, edited) {
		t.Error("переписанный A не тронут")
	}
	if m := loadLKMarks(); len(m) != 1 || filepath.Base(m[0].Path) != "patch-ruRU-B.MPQ" {
		t.Errorf("помечен только B: %+v", m)
	}
}

func TestLKRefuse(t *testing.T) {
	stubGame(t, false)
	stubUserConfig(t)
	asset, stock := etalon(t)
	cases := []struct {
		what  string
		files map[string][]byte
		want  string
	}{
		{"Z с DBC — мешает Z", map[string][]byte{"ruRU/patch-ruRU-3.MPQ": chunkMPQ(stock), "ruRU/patch-ruRU-Z.MPQ": chunkMPQ(stock)},
			"Не поставлен: после " + dataRel("ruRU", "patch-ruRU-Z.MPQ") + " свободной буквы нет — уберите или переименуйте этот архив."},
		{"этаж 109 не ЦЛК", map[string][]byte{"ruRU/patch-ruRU-3.MPQ": chunkMPQ(stock), "ruRU/patch-ruRU-2.MPQ": writeMPQ(mpqEntry{lkDMap, dmapDBC([2]uint32{109, 600})})},
			"Не поставлен: в " + dataRel("ruRU", "patch-ruRU-2.MPQ") + " этаж 109 — карта 600, а не ЦЛК (631)."},
		{"нет этажа 109", map[string][]byte{"ruRU/patch-ruRU-3.MPQ": chunkMPQ(stock), "ruRU/locale-ruRU.MPQ": writeMPQ(mpqEntry{lkDMap, dmapDBC([2]uint32{110, 631})})},
			"Не поставлен: в " + dataRel("ruRU", "locale-ruRU.MPQ") + " нет этажа 109."},
		{"нет DungeonMap.dbc", map[string][]byte{"ruRU/patch-ruRU-3.MPQ": chunkMPQ(stock), "ruRU/locale-ruRU.MPQ": otherMPQ()},
			"Не поставлен: DungeonMap.dbc нет ни в одном архиве игры — этаж 109 не сверить."},
		{"нет DungeonMapChunk.dbc", map[string][]byte{"ruRU/patch-ruRU-3.MPQ": otherMPQ()},
			"Не поставлен: DungeonMapChunk.dbc нет ни в одном архиве игры."},
		{"чужой общий patch-A", map[string][]byte{"patch-A.MPQ": chunkMPQ(stock), "ruRU/patch-ruRU-3.MPQ": chunkMPQ(withRows(stock, [4]uint32{1828, 631, 28150, 109}, [4]uint32{1829, 631, 28151, 109}))},
			dataRel("patch-A.MPQ") + " — не наш, не трогали.\n" + dataRel("patch-A.MPQ") + " перекрыт старшим " + dataRel("ruRU", "patch-ruRU-3.MPQ") + " — клиент видит только его таблицу.\nПозиции на Личе в порядке: строки платформы уже есть в " + dataRel("ruRU", "patch-ruRU-3.MPQ") + "."},
	}
	for _, c := range cases {
		game := lkClient(t, c.files)
		before := gameFiles(t, game)
		res, err := lkPatch(game)
		if err != nil || res.text != c.want {
			t.Errorf("%s:\n%q\nнужно\n%q\n%v", c.what, res.text, c.want, err)
		}
		if !reflect.DeepEqual(before, gameFiles(t, game)) {
			t.Errorf("%s: ничего не тронуто", c.what)
		}
	}

	game := lkClient(t, map[string][]byte{"patch-A.MPQ": asset, "ruRU/patch-ruRU-3.MPQ": chunkMPQ(stock)})
	stubGame(t, true)
	before := gameFiles(t, game)
	if res, err := lkPatch(game); err != nil || res.kind != lkSkip || res.text != lkRunning || !reflect.DeepEqual(before, gameFiles(t, game)) {
		t.Errorf("игра запущена — ничего не тронуто: %q %v", res.text, err)
	}
	if res, _ := lkPatch(t.TempDir()); res.kind != lkSkip || res.text != "Клиент игры не найден: рядом с Interface\\AddOns нет папки Data." {
		t.Errorf("нет клиента: %q", res.text)
	}
	noLoc := t.TempDir()
	_ = os.MkdirAll(filepath.Join(noLoc, "Data"), 0o755)
	if res, _ := lkPatch(noLoc); res.kind != lkSkip || res.text != "Локаль клиента не найдена: в Data нет папки вроде ruRU с locale-ruRU.MPQ." {
		t.Errorf("нет локали: %q", res.text)
	}
}

func waitLK(t *testing.T, a *app) (string, int) {
	t.Helper()
	for i := 0; i < 500; i++ {
		if busy, status, kind := a.lkView(); !busy {
			return status, kind
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("проверка не закончилась")
	return "", 0
}

func TestLKButtonAndAuto(t *testing.T) {
	retryPause = 0
	stubGame(t, false)
	_, stock := etalon(t)
	list := []ghRelease{relWith("v0.2.0", false, zipName("v0.2.0"), liteName("v0.2.0"))}
	zips := map[string][]byte{
		zipName("v0.2.0"):  rhZip(t, "0.2.0"),
		liteName("v0.2.0"): rhZip(t, "0.2.0"),
	}
	srv, _ := releaseServerHits(t, list, zips)
	oldRepo := raidHelperRepo
	raidHelperRepo = srv.URL + "/releases"
	t.Cleanup(func() { raidHelperRepo = oldRepo })
	root, dir := layout(t)
	stubUserConfig(t)
	put(t, filepath.Join(root, "WTF", "Config.wtf"), "SET locale \"ruRU\"\r\n")
	put(t, filepath.Join(root, "Data", "ruRU", "locale-ruRU.MPQ"), string(writeMPQ(mpqEntry{lkDMap, dmapDBC([2]uint32{109, 631})})))
	put(t, filepath.Join(root, "Data", "ruRU", "patch-ruRU-3.MPQ"), string(chunkMPQ(stock)))
	rh := raidHelper(dir)
	addons := filepath.Dir(rh.folder)
	ours := filepath.Join(root, "Data", "ruRU", "patch-ruRU-A.MPQ")
	a := &app{addons: addons}

	a.lkFix(true)
	if status, _ := waitLK(t, a); status != "" || exists(ours) {
		t.Fatalf("без Raid Helper автопроверки нет: %q", status)
	}
	lkAuto(addons)
	if exists(ours) || strings.Contains(read(filepath.Join(logsDir(), logFile)), "Лич") {
		t.Fatal("без Raid Helper фоновая проверка молчит")
	}
	if res, err := syncRH(checkOther(rh, false), true, map[string]bool{}, &counter{}); err != nil || rh.localVersion() != "0.2.0" || strings.Contains(res.text(), "Лич") || exists(ours) {
		t.Fatalf("установка Raid Helper патч не трогает: %q %v", res.text(), err)
	}

	stubGame(t, true)
	a.lkFix(true)
	if status, kind := waitLK(t, a); status != lkWait || kind != kindNone || exists(ours) {
		t.Fatalf("автопроверка при запущенной игре ждёт выхода: %q %d", status, kind)
	}
	lkAuto(addons)
	if exists(ours) || !strings.Contains(loadConfig().auto().Log[noteLK].Text, lkWait) {
		t.Fatalf("фон при запущенной игре ждёт: %+v", loadConfig().auto().Log[noteLK])
	}
	a.lkFix(false)
	if status, kind := waitLK(t, a); status != lkRunning || kind != kindErr || exists(ours) {
		t.Fatalf("кнопка при запущенной игре — закройте игру: %q %d", status, kind)
	}

	stubGame(t, false)
	lkAuto(addons)
	if !exists(ours) || !strings.HasPrefix(loadConfig().auto().Log[noteLK].Text, "позиции на Личе: Поставлен "+dataRel("ruRU", "patch-ruRU-A.MPQ")) {
		t.Fatalf("после выхода из игры фон ставит: %+v", loadConfig().auto().Log[noteLK])
	}
	a.lkFix(true)
	if status, kind := waitLK(t, a); !strings.HasPrefix(status, "Позиции на Личе в порядке: всё на месте") || kind != kindOK {
		t.Fatalf("автопроверка окна — тот же статус: %q %d", status, kind)
	}
	a.lkFix(false)
	if status, kind := waitLK(t, a); !strings.HasPrefix(status, "Позиции на Личе в порядке: всё на месте") || kind != kindOK {
		t.Fatalf("кнопка — тот же статус: %q %d", status, kind)
	}

	unlock, err := lockInstall(addons)
	if err != nil {
		t.Fatal(err)
	}
	a.lkFix(false)
	if status, _ := waitLK(t, a); !strings.HasPrefix(status, "Другая копия обновлялки") {
		t.Errorf("замок занят другой копией: %q", status)
	}
	unlock()
}

func linkTree(t *testing.T, src, dst string) {
	t.Helper()
	ents, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".mpq") {
			continue
		}
		from, to := filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())
		if os.Link(from, to) == nil {
			continue
		}
		in, err := os.Open(from)
		if err != nil {
			t.Fatal(err)
		}
		out, err := os.Create(to)
		if err == nil {
			_, err = io.Copy(out, in)
			out.Close()
		}
		in.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
}

func statTree(root string) map[string]string {
	out := map[string]string{}
	_ = filepath.Walk(filepath.Join(root, "Data"), func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			out[p] = fmt.Sprint(info.Size(), info.ModTime().UnixNano())
		}
		return nil
	})
	return out
}

func clientCopy(t *testing.T, src string, skip ...string) string {
	t.Helper()
	game := t.TempDir()
	cfg, err := os.ReadFile(filepath.Join(src, "WTF", "Config.wtf"))
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(game, "WTF", "Config.wtf"), string(cfg))
	linkTree(t, filepath.Join(src, "Data"), filepath.Join(game, "Data"))
	for _, l := range mpqLocales(filepath.Join(src, "Data")) {
		linkTree(t, filepath.Join(src, "Data", l), filepath.Join(game, "Data", l))
	}
	for _, s := range skip {
		_ = os.Remove(filepath.Join(game, "Data", filepath.FromSlash(s)))
	}
	return game
}

func TestLKCustomerClient(t *testing.T) {
	src := os.Getenv("LK_CLIENT")
	if src == "" {
		t.Skip("LK_CLIENT не задан")
	}
	stubGame(t, false)
	stubUserConfig(t)
	before := statTree(src)
	tried := locPath(src, "patch-ruRU-A.MPQ")

	game := clientCopy(t, src)
	res, err := lkPatch(game)
	t.Log("как есть: " + res.text)
	if _, dbc := activeChunk(t, game); err != nil || dbc == nil || len(lkMissing(func() [][]byte { r, _, _ := chunkRows(dbc); return r }())) != 0 {
		t.Fatalf("после прохода строки платформы действуют: %v", err)
	}

	game = clientCopy(t, src, "ruRU/patch-ruRU-A.MPQ")
	saveLKMarks(nil)
	res, err = lkPatch(game)
	t.Log("без patch-ruRU-A: " + res.text)
	if err != nil || res.kind != lkPut || !strings.Contains(res.text, "Поставлен "+dataRel("ruRU", "patch-ruRU-A.MPQ")+" поверх "+dataRel("ruRU", "patch-ruRU-3.MPQ")+" (622 → 624 строки)") {
		t.Fatalf("%+v %v", res, err)
	}
	base, _ := mpqRead(locPath(game, "patch-ruRU-3.MPQ"), lkChunk)
	_, dbc := activeChunk(t, game)
	at, errs := lkPlace(dbc, base)
	t.Log("новые строки на местах", at)
	if len(errs) > 0 || !reflect.DeepEqual(at, []int{599, 600}) {
		t.Fatalf("места строк: %v %v", at, errs)
	}
	if want, err := os.ReadFile(tried); err == nil {
		got, _ := os.ReadFile(locPath(game, "patch-ruRU-A.MPQ"))
		t.Logf("сверка с проверенным в игре %s: %v", tried, bytes.Equal(got, want))
	}
	if res, err := lkPatch(game); err != nil || res.kind != lkHave {
		t.Fatalf("повтор: %+v %v", res, err)
	}
	if !reflect.DeepEqual(before, statTree(src)) {
		t.Fatal("клиент игрока изменился")
	}
}
