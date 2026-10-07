package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const svPost = `{
		["v"] = 2,
		["posts"] = {
			{
				["id"] = "1790700000-3-1790712000",
				["raid"] = "ЦЛК 25 гер.",
				["zone"] = "Цитадель Ледяной Короны",
				["size"] = 25,
				["heroic"] = true,
				["start"] = 1790700000,
				["finish"] = 1790712000,
				["combat"] = 5400,
				["idle"] = 6600,
				["deaths"] = 9,
				["damage"] = 912345678,
				["healed"] = 201234567,
				["cost"] = 45503260,
				["bosses"] = {
					{
						["name"] = "Лорд Ребрад",
						["kill"] = true,
						["tries"] = 1,
						["wipes"] = 0,
						["time"] = 105,
						["start"] = 1790700600,
						["damage"] = 31234567,
						["deaths"] = 0,
						["dps"] = {
							{
								["n"] = "Тестмаг",
								["c"] = "MAGE",
								["v"] = 21000,
								["t"] = 2205000,
							}, -- [1]
							{
								["n"] = "Name \"quoted\" \\ back <b>",
								["c"] = "ROGUE",
								["v"] = 18000.5,
								["t"] = 1890000,
							}, -- [2]
						},
						["hps"] = {
							{
								["n"] = "Лекарь",
								["c"] = "PRIEST",
								["v"] = 7000,
								["t"] = 735000,
							}, -- [1]
						},
						["targets"] = {
							{
								["title"] = "Урон по Костяным шипам",
								["unit"] = "dmg",
								["rows"] = {
									{
										["n"] = "Тестмаг",
										["c"] = "MAGE",
										["v"] = 120000,
										["s"] = "12%",
									}, -- [1]
								},
							}, -- [1]
						},
					}, -- [1]
					{
						["name"] = "Король-лич",
						["kill"] = false,
						["tries"] = 2,
						["wipes"] = 2,
						["time"] = 412,
						["start"] = 1790710000,
						["damage"] = 101234567,
						["deaths"] = 9,
						["dps"] = {
							{
								["n"] = "Танк",
								["c"] = "WARRIOR",
								["v"] = 9000,
								["t"] = 3708000,
							}, -- [1]
						},
					}, -- [2]
				},
				["immortal"] = {
					{
						["n"] = "Тестмаг",
						["c"] = "MAGE",
						["tries"] = 3,
					}, -- [1]
				},
				["rod"] = {
					{
						["n"] = "Танк",
						["c"] = "WARRIOR",
						["v"] = 7,
						["s"] = "выбран 7 раз",
					}, -- [1]
				},
				["buffed"] = {
					{
						["n"] = "Тестмаг",
						["c"] = "MAGE",
						["v"] = 41,
						["s"] = 41,
					}, -- [1]
				},
				["consumables"] = {
					{
						["k"] = "flask",
						["label"] = "Банки",
						["v"] = 31,
					}, -- [1]
				},
			}, -- [1]
		},
	}`

func svFile(withLead bool, crlf bool) []byte {
	var b strings.Builder
	b.WriteString("\nManaCodeRaidHelperDB = {\n")
	b.WriteString("\t[\"segments\"] = {\n\t\t{\n\t\t\t[\"discord\"] = {\n\t\t\t\t[\"v\"] = 9,\n\t\t\t},\n")
	b.WriteString("\t\t\t[\"text\"] = \"\\n\\t[\\\"discord\\\"] = {\",\n")
	for i := 0; i < 2000; i++ {
		b.WriteString("\t\t\t\"chunk 0123456789 abcdefghij klmnopqrst\", -- [1]\n")
	}
	b.WriteString("\t\t}, -- [1]\n\t},\n")
	b.WriteString("\t[\"recording\"] = true,\n")
	b.WriteString("\t[\"discord\"] = " + svPost + ",\n")
	b.WriteString("\t[\"fxVersion\"] = 5,\n}\n")
	if withLead {
		b.WriteString("ManaCodeRaidLeadDB = {\n\t[\"discord\"] = {\n\t\t[\"v\"] = 2,\n\t\t[\"posts\"] = {},\n\t},\n}\n")
	}
	s := b.String()
	if crlf {
		s = strings.ReplaceAll(s, "\n", "\r\n")
	}
	return []byte(s)
}

func TestReadDiscordFindsTopKey(t *testing.T) {
	for _, crlf := range []bool{false, true} {
		posts, err := readDiscord(svFile(true, crlf))
		if err != nil {
			t.Fatalf("crlf=%v: %v", crlf, err)
		}
		if len(posts) != 1 {
			t.Fatalf("постов %d", len(posts))
		}
		p := posts[0]
		if p.ID != "1790700000-3-1790712000" || p.Raid != "ЦЛК 25 гер." || p.Zone != "Цитадель Ледяной Короны" || p.Size != 25 ||
			!p.Heroic || p.Start != 1790700000 || p.Finish != 1790712000 || p.Combat != 5400 || p.Idle != 6600 || p.Deaths != 9 ||
			p.Damage != 912345678 || p.Healed != 201234567 || !p.HasCost || p.Cost != 45503260 {
			t.Errorf("шапка: %+v", p)
		}
		if len(p.Bosses) != 2 || !p.Bosses[0].Kill || p.Bosses[1].Kill || p.Bosses[1].Wipes != 2 || p.Bosses[1].Start != 1790710000 {
			t.Fatalf("боссы: %+v", p.Bosses)
		}
		b := p.Bosses[0]
		if len(b.DPS) != 2 || b.DPS[0].Class != "MAGE" || b.DPS[1].Name != `Name "quoted" \ back <b>` || b.DPS[1].V != 18000.5 || b.DPS[1].T != 1890000 {
			t.Errorf("ДПС: %+v", b.DPS)
		}
		if len(b.HPS) != 1 || b.HPS[0].V != 7000 {
			t.Errorf("ХПС: %+v", b.HPS)
		}
		if len(b.Targets) != 1 || b.Targets[0].Unit != "dmg" || b.Targets[0].Rows[0].S != "12%" {
			t.Errorf("цели: %+v", b.Targets)
		}
		if len(p.Immortal) != 1 || p.Immortal[0].Tries != 3 || len(p.Rod) != 1 || p.Rod[0].S != "выбран 7 раз" ||
			len(p.Buffed) != 1 || p.Buffed[0].S != "41" || len(p.Consumables) != 1 || p.Consumables[0].Key != "flask" {
			t.Errorf("блоки: %+v %+v %+v %+v", p.Immortal, p.Rod, p.Buffed, p.Consumables)
		}
	}
}

func TestReadDiscordMissing(t *testing.T) {
	src := []byte("\nManaCodeRaidHelperDB = {\n\t[\"recording\"] = true,\n}\nManaCodeRaidLeadDB = {\n\t[\"discord\"] = " + svPost + ",\n}\n")
	if _, err := readDiscord(src); err != errNoTable {
		t.Errorf("ключ чужой таблицы не наш: %v", err)
	}
	old := strings.Replace(string(svFile(false, false)), "[\"v\"] = 2,", "[\"v\"] = 1,", 1)
	if _, err := readDiscord([]byte(old)); err == nil {
		t.Error("v1 больше не читаем")
	}
	if _, err := readDiscord([]byte("\nManaCodeRaidHelperDB = {\n\t[\"discord\"] = {\n\t\t[\"v\"] = 2,\n")); err == nil {
		t.Error("оборванная таблица — ошибка")
	}
	noCost := strings.Replace(string(svFile(false, false)), "\t\t\t\t[\"cost\"] = 45503260,\n", "", 1)
	if posts, err := readDiscord([]byte(noCost)); err != nil || posts[0].HasCost {
		t.Errorf("cost=nil: %v %+v", err, posts)
	}
}

func TestLuaStrings(t *testing.T) {
	p := &luaParser{s: []byte(`{ "a\"b", 'c\'d', "e\\f", "g\nh", "\208\159", x = 1, [3] = -2.5e1, ["k"] = false }`)}
	v, err := p.value()
	if err != nil {
		t.Fatal(err)
	}
	tb := v.(*luaTable)
	want := []any{`a"b`, `c'd`, `e\f`, "g\nh", "П", -25.0}
	if len(tb.arr) != 5 {
		t.Fatalf("массив: %#v", tb.arr)
	}
	for i := 0; i < 5; i++ {
		if tb.arr[i] != want[i] {
			t.Errorf("[%d] = %#v, ждали %#v", i+1, tb.arr[i], want[i])
		}
	}
	if tb.num("x") != 1 || tb.get("k") != false || tb.get("3") != -25.0 {
		t.Errorf("ключи: %#v", tb.m)
	}
}

func samplePost(t *testing.T) discordPost {
	t.Helper()
	posts, err := readDiscord(svFile(false, false))
	if err != nil || len(posts) != 1 {
		t.Fatalf("образец: %v", err)
	}
	return posts[0]
}

func bigPost(t *testing.T) discordPost {
	t.Helper()
	posts, err := readDiscord([]byte(samplePostLua()))
	if err != nil || len(posts) != 1 {
		t.Fatalf("большой образец: %v", err)
	}
	return posts[0]
}

func TestBigSample(t *testing.T) {
	p := bigPost(t)
	if len(p.Bosses) != 12 || len(p.Bosses[0].DPS) != 25 || p.Bosses[11].Kill || p.Bosses[11].Wipes != 5 || len(p.Bosses[11].Targets) != 2 {
		t.Fatalf("образец ЦЛК: боссов %d", len(p.Bosses))
	}
}

func TestRenderPost(t *testing.T) {
	p := samplePost(t)
	pic, err := renderPost(p)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(pic))
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	if b.Dx() != pngPad*2+pngCols*pngCardW+pngGap || b.Dy() < pngHeadH+cardHeight(p.Bosses[0]) {
		t.Errorf("размер %v", b)
	}
	if c := img.At(pngPad+50, pngHeadH+pngBossH+8); c == img.At(pngPad+50+pngCardW-60, pngHeadH+pngBossH+8) {
		t.Errorf("полоска первого места не залита цветом класса")
	}
}

func TestPostPages(t *testing.T) {
	p := bigPost(t)
	art := pageArt{Font: []byte("\x00\x01\x00\x00font"), Icons: map[string]string{"inv_misc_fish_52": "data:image/png;base64,FISH"},
		Classes: map[string]string{"MAGE": "data:image/png;base64,MAGE"}, Gold: "data:image/png;base64,GOLD"}
	pages := postPages(p, art)
	if len(pages) != 7 {
		t.Fatalf("страниц %d: итог + 12 по 2 — ДПС на 25 не влезает в 4 карточки", len(pages))
	}
	sum := pages[0].HTML
	for _, want := range []string{"Итог рейда: Цитадель Ледяной Короны", "25 гер.", "Бессмертные", "Громоотвод", "Обмазанный",
		"Расходники", "1&#8239;456", "Король-лич", "pill wipe", `font-family:"Friz"`, "data:font/ttf;base64,",
		"Топ-5 рейда", "Теневор", `<img class="ic" src="data:image/png;base64,FISH">`, "Рыбный пир", `<span class="ic no">`,
		"нет цены", `src="data:image/png;base64,GOLD"`, `.c-MAGE{background-image:url(data:image/png;base64,MAGE)}`,
		`<span class="ci c-MAGE"></span>Огнеплёт`, "grid-template-columns:minmax(0,1fr) minmax(0,1fr)"} {
		if !strings.Contains(sum, want) {
			t.Errorf("в итоге нет %q", want)
		}
	}
	if pages[0].W != 1920 || pages[0].H != 1080 {
		t.Errorf("итог %d×%d", pages[0].W, pages[0].H)
	}
	for i, pg := range pages[1:] {
		if n := strings.Count(pg.HTML, `class="card bcard"`); n != 2 {
			t.Errorf("страница %d: карточек %d", i+2, n)
		}
	}
	for i, b := range p.Bosses {
		pg := pages[1+i/2].HTML
		for _, r := range b.DPS {
			if !strings.Contains(pg, esc(r.Name)) {
				t.Errorf("%s: в ДПС нет %s", b.Name, r.Name)
			}
		}
		if len(b.DPS) != 25 || len(b.HPS) != 6 {
			t.Errorf("%s: ДПС %d, ХПС %d", b.Name, len(b.DPS), len(b.HPS))
		}
	}
	if !strings.Contains(pages[6].HTML, "Урон по Валь&#39;кирам") || !strings.Contains(pages[1].HTML, "Боссы 1–2 из 12") {
		t.Error("цели или шапка страницы боссов")
	}
	p.Top = nil
	p.Consumables = []discordConsumable{{Key: "flask", Label: "Банки", V: 31}}
	if s := postPages(p, pageArt{})[0].HTML; strings.Contains(s, "Топ-5") || !strings.Contains(s, "Банки") || strings.Contains(s, ".c-MAGE{") ||
		strings.Contains(s, `class="it"`) {
		t.Error("без top и items — без топа и категориями; без иконок классов — без правил .c-*")
	}
	small := samplePost(t)
	sp := postPages(small, pageArt{})
	if len(sp) != 2 || sp[1].H != htmlHalfH || strings.Contains(sp[0].HTML, "Friz") {
		t.Errorf("два босса — одна половинная страница, без шрифта игры — Segoe: %d", len(sp))
	}
	if strings.Contains(sp[1].HTML, `<b>"quoted"`) || !strings.Contains(sp[1].HTML, "&lt;b&gt;") {
		t.Error("имена не экранированы")
	}
}

func TestPicBatches(t *testing.T) {
	mk := func(sizes ...int) []discordPic {
		var out []discordPic
		for i, s := range sizes {
			out = append(out, discordPic{Name: fmt.Sprint(i), Data: make([]byte, s)})
		}
		return out
	}
	if b := picBatches(mk(1, 1, 1, 1)); len(b) != 1 || len(b[0]) != 4 {
		t.Errorf("4 малых — одним: %d", len(b))
	}
	if b := picBatches(mk(3<<20, 3<<20, 3<<20, 1<<20)); len(b) != 2 || len(b[0]) != 2 || len(b[1]) != 2 {
		t.Errorf("по 8 МБ: %v", len(b))
	}
	if b := picBatches(mk(make([]int, 12)...)); len(b) != 2 || len(b[0]) != 10 {
		t.Errorf("по 10 файлов: %v", len(b))
	}
	if b := picBatches(mk(9 << 20)); len(b) != 1 {
		t.Errorf("одна большая — всё равно шлём: %v", len(b))
	}
}

func TestRenderSamples(t *testing.T) {
	dir := os.Getenv("DISCORD_SAMPLE_DIR")
	if dir == "" {
		t.Skip("DISCORD_SAMPLE_DIR не задан")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := []byte(samplePostLua())
	if err := os.WriteFile(filepath.Join(dir, "discord_v2.lua"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	p := bigPost(t)
	addons := os.Getenv("DISCORD_SAMPLE_ADDONS")
	start := time.Now()
	pics, err := renderPics(addons, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(pics) != 7 {
		t.Fatalf("картинок %d — браузер не нашёлся?", len(pics))
	}
	for _, pic := range pics {
		cfg, err := png.DecodeConfig(bytes.NewReader(pic.Data))
		if err != nil || cfg.Width != 3840 {
			t.Errorf("%s: %v %dx%d", pic.Name, err, cfg.Width, cfg.Height)
		}
		if err := os.WriteFile(filepath.Join(dir, pic.Name), pic.Data, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("%s %d×%d %d КБ", pic.Name, cfg.Width, cfg.Height, len(pic.Data)>>10)
	}
	for i, pg := range postPages(p, loadArt(gameDir(addons), p)) {
		_ = os.WriteFile(filepath.Join(dir, fmt.Sprintf("page-%d.html", i+1)), []byte(pg.HTML), 0o644)
	}
	t.Logf("за %v", time.Since(start))
}

type hookServer struct {
	mu     sync.Mutex
	calls  int
	status int
	failAt int
	got    []string
	files  []int
	pics   [][]byte
}

func newHook(t *testing.T) (*hookServer, *httptest.Server) {
	h := &hookServer{status: http.StatusNoContent}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.calls++
		if r.Method != http.MethodPost {
			t.Errorf("метод %s", r.Method)
		}
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			t.Errorf("не multipart: %v", err)
		}
		var payload struct {
			Content     string           `json:"content"`
			Attachments []map[string]any `json:"attachments"`
		}
		if err := json.Unmarshal([]byte(r.FormValue("payload_json")), &payload); err != nil {
			t.Errorf("payload_json: %v", err)
		}
		h.got = append(h.got, payload.Content)
		n := 0
		for i := 0; ; i++ {
			f, fh, err := r.FormFile(fmt.Sprintf("files[%d]", i))
			if err != nil {
				break
			}
			b, _ := io.ReadAll(f)
			h.pics = append(h.pics, b)
			if fh.Header.Get("Content-Type") != "image/png" || !bytes.HasPrefix(b, []byte("\x89PNG")) {
				t.Errorf("файл не PNG: %s", fh.Header.Get("Content-Type"))
			}
			n++
		}
		if n == 0 || len(payload.Attachments) != n {
			t.Errorf("файлов %d, attachments %d", n, len(payload.Attachments))
		}
		h.files = append(h.files, n)
		status := h.status
		if h.failAt > 0 && h.calls == h.failAt {
			status = http.StatusInternalServerError
		}
		w.WriteHeader(status)
		if status == http.StatusTooManyRequests {
			_, _ = w.Write([]byte(`{"message":"You are being rate limited.","retry_after":1.5}`))
		}
	}))
	t.Cleanup(srv.Close)
	return h, srv
}

func fakePics(sizes ...int) []discordPic {
	var out []discordPic
	for i, s := range sizes {
		b := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, s)...)
		out = append(out, discordPic{Name: fmt.Sprintf("raid-%d.png", i+1), Data: b})
	}
	return out
}

func TestPostWebhook(t *testing.T) {
	h, srv := newHook(t)
	p := samplePost(t)
	pics := fakePics(100, 100, 100)
	if n, err := postWebhook(srv.URL, p, pics, nil, 0); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	want := "**ЦЛК 25 гер.** — " + time.Unix(1790700000, 0).Format("02.01.2006")
	if h.calls != 1 || h.files[0] != 3 || !strings.HasPrefix(h.got[0], want) || !strings.Contains(h.got[0], "убито 1 из 2") ||
		!strings.Contains(h.got[0], "вайпов 2") {
		t.Errorf("сообщение: %q, файлов %v", h.got, h.files)
	}
	h.status = http.StatusTooManyRequests
	if _, err := postWebhook(srv.URL, p, pics, nil, 0); err == nil || !strings.Contains(err.Error(), "подождать") {
		t.Errorf("429: %v", err)
	}
	h.status = http.StatusNotFound
	if _, err := postWebhook(srv.URL, p, pics, nil, 0); err == nil || !strings.Contains(err.Error(), "вебхук") {
		t.Errorf("404: %v", err)
	}
}

func TestPostWebhookDivider(t *testing.T) {
	h, srv := newHook(t)
	p := samplePost(t)
	pics := fakePics(3<<20, 3<<20, 3<<20)
	tail := fakePics(10)
	n, err := postWebhook(srv.URL, p, pics, tail, 0)
	if err != nil || n != 3 || h.calls != 3 {
		t.Fatalf("итог двумя сообщениями и разделитель третьим: n=%d err=%v вызовов %d", n, err, h.calls)
	}
	if h.got[2] != "" || h.files[2] != 1 || h.files[1] != 1 || h.files[0] != 2 {
		t.Errorf("разделитель — отдельное последнее сообщение без текста: %q %v", h.got, h.files)
	}
}

func TestPostWebhookSplits(t *testing.T) {
	h, srv := newHook(t)
	p := samplePost(t)
	pics := fakePics(3<<20, 3<<20, 3<<20, 1<<20)
	h.failAt = 2
	n, err := postWebhook(srv.URL, p, pics, nil, 0)
	if err == nil || n != 1 || h.calls != 2 {
		t.Fatalf("второе сообщение упало: n=%d err=%v вызовов %d", n, err, h.calls)
	}
	n, err = postWebhook(srv.URL, p, pics, nil, n)
	if err != nil || n != 2 || h.calls != 3 {
		t.Fatalf("повтор с места обрыва: n=%d err=%v", n, err)
	}
	if h.got[0] == "" || h.got[1] != "" || h.got[2] != "" || h.files[0] != 2 || h.files[2] != 2 {
		t.Errorf("текст только в первом, файлы по 8 МБ: %q %v", h.got, h.files)
	}
}

func TestValidWebhook(t *testing.T) {
	ok := []string{"https://discord.com/api/webhooks/123/abc-DEF_9", "https://canary.discord.com/api/webhooks/1/x",
		" https://discordapp.com/api/webhooks/1/x "}
	bad := []string{"http://discord.com/api/webhooks/1/x", "https://evil.com/api/webhooks/1/x",
		"https://discord.com/api/webhooks/1/x?wait=true", ""}
	for _, u := range ok {
		if !validWebhook(u) {
			t.Errorf("должен подойти: %q", u)
		}
	}
	for _, u := range bad {
		if validWebhook(u) {
			t.Errorf("не должен подойти: %q", u)
		}
	}
	if err := setWebhook("https://example.com/x"); err == nil {
		t.Error("чужой адрес не сохраняем")
	}
}

type discordEnv struct {
	addons string
	sv     string
	clock  time.Time
}

func newDiscordEnv(t *testing.T) *discordEnv {
	t.Helper()
	root := t.TempDir()
	cfg := filepath.Join(root, "cfg")
	oldCfg, oldNow, oldBrowser := userConfigDir, now, browserPath
	browserPath = func() string { return "" }
	userConfigDir = func() (string, error) { return cfg, nil }
	e := &discordEnv{addons: filepath.Join(root, "game", "Interface", "AddOns"), clock: time.Unix(1790712200, 0)}
	now = func() time.Time { return e.clock }
	t.Cleanup(func() { userConfigDir, now, browserPath = oldCfg, oldNow, oldBrowser })
	if err := os.MkdirAll(e.addons, 0o755); err != nil {
		t.Fatal(err)
	}
	e.sv = filepath.Join(root, "game", "WTF", "Account", "ACC", "SavedVariables", rhSVFile)
	if err := os.MkdirAll(filepath.Dir(e.sv), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.sv, svFile(true, true), 0o644); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *discordEnv) touch(t *testing.T, extra string) {
	t.Helper()
	b, _ := os.ReadFile(e.sv)
	if err := os.WriteFile(e.sv, append(b, extra...), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPollSendsOnce(t *testing.T) {
	e := newDiscordEnv(t)
	h, srv := newHook(t)
	st := loadDiscord()
	st.Webhook = srv.URL
	if err := saveDiscord(st); err != nil {
		t.Fatal(err)
	}
	w := newDiscordWatch()
	res := w.poll(e.addons)
	if len(res) != 1 || !res[0].sent || res[0].err != nil || h.calls != 1 {
		t.Fatalf("первый проход: %+v, вызовов %d", res, h.calls)
	}
	if !strings.HasPrefix(res[0].text(), "Discord: отправлен итог «ЦЛК 25 гер., "+time.Unix(1790700000, 0).Format("02.01.2006")+"»") {
		t.Errorf("текст: %s", res[0].text())
	}
	if got := w.poll(e.addons); len(got) != 0 || h.calls != 1 {
		t.Fatalf("повтор без изменений: %+v", got)
	}
	e.touch(t, "\n")
	if got := newDiscordWatch().poll(e.addons); len(got) != 0 || h.calls != 1 {
		t.Fatalf("после /reload тот же id не шлём: %+v", got)
	}
	if loadDiscord().Done["1790700000-3-1790712000"] != discordSent {
		t.Error("id не помечен")
	}
	r, ok := w.again(e.addons)
	if !ok || !r.sent || h.calls != 2 {
		t.Errorf("«Отправить ещё раз»: %+v", r)
	}
}

func TestPollRetriesAfterFail(t *testing.T) {
	e := newDiscordEnv(t)
	h, srv := newHook(t)
	h.status = http.StatusInternalServerError
	st := loadDiscord()
	st.Webhook = srv.URL
	_ = saveDiscord(st)
	w := newDiscordWatch()
	if res := w.poll(e.addons); len(res) != 1 || res[0].err == nil {
		t.Fatalf("500 — ошибка: %+v", res)
	}
	if res := w.poll(e.addons); len(res) != 0 || h.calls != 1 {
		t.Fatalf("сразу не повторяем: %+v", res)
	}
	h.status = http.StatusOK
	e.clock = e.clock.Add(discordRetry + time.Second)
	if res := w.poll(e.addons); len(res) != 1 || !res[0].sent || h.calls != 2 {
		t.Fatalf("повтор через %v: %+v", discordRetry, res)
	}
}

func TestPollSavesWithoutWebhook(t *testing.T) {
	e := newDiscordEnv(t)
	w := newDiscordWatch()
	res := w.poll(e.addons)
	if len(res) != 1 || res[0].sent || res[0].err != nil || res[0].file == "" {
		t.Fatalf("без вебхука: %+v", res)
	}
	want := filepath.Join(e.addons, "..", "..", "Screenshots", discordPicDir, "raid-1790700000-3-1790712000.png")
	if filepath.Clean(res[0].file) != filepath.Clean(want) || !exists(want) {
		t.Errorf("файл %s, ждали %s", res[0].file, want)
	}
	if !strings.Contains(res[0].text(), "сохранён: ") {
		t.Errorf("текст: %s", res[0].text())
	}
	if loadDiscord().Done["1790700000-3-1790712000"] != discordSaved {
		t.Error("сохранённый помечен")
	}
	if got := w.poll(e.addons); len(got) != 0 {
		t.Errorf("второй раз не сохраняем: %+v", got)
	}
}

func TestStateKeepsLast(t *testing.T) {
	var st discordState
	for i := 0; i < discordKeep+5; i++ {
		st.mark(time.Unix(int64(i), 0).String(), discordSent)
	}
	if len(st.Done) != discordKeep || len(st.Order) != discordKeep {
		t.Errorf("помним %d/%d", len(st.Done), len(st.Order))
	}
}

func TestMaskHook(t *testing.T) {
	cases := map[string]string{
		"https://discord.com/api/webhooks/123/abcDEF":                   "https://discord.com/api/webhooks/123/••••••",
		"https://discord.com/api/webhooks/123/":                         "https://discord.com/api/webhooks/123/",
		"https://discord.com/api/webho":                                 "https://discord.com/api/webho",
		"https://discord.com/api/webhooks/1/" + strings.Repeat("x", 68): "https://discord.com/api/webhooks/1/" + strings.Repeat("•", 12),
	}
	for in, want := range cases {
		if got := maskHook(in); got != want {
			t.Errorf("maskHook(%q) = %q, ждали %q", in, got, want)
		}
	}
}

func TestAppDiscordView(t *testing.T) {
	cfg := t.TempDir()
	old := userConfigDir
	userConfigDir = func() (string, error) { return cfg, nil }
	t.Cleanup(func() { userConfigDir = old })
	a := &app{dc: newDiscordWatch()}
	if v := a.discordView(); v.status != dcNoHook || v.againOn {
		t.Errorf("без вебхука и без игры: %+v", v)
	}
	if a.saveHook("https://example.com/x") {
		t.Error("чужой адрес сохранился")
	}
	if !a.saveHook(" https://discord.com/api/webhooks/1/tok ") || loadDiscord().Webhook != "https://discord.com/api/webhooks/1/tok" {
		t.Errorf("вебхук не сохранён: %q", loadDiscord().Webhook)
	}
	if v := a.discordView(); v.hook != "https://discord.com/api/webhooks/1/tok" || v.kind != kindOK {
		t.Errorf("после сохранения: %+v", v)
	}
}
