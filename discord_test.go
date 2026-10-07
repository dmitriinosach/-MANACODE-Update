package main

import (
	"bytes"
	"encoding/json"
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
		["v"] = 1,
		["posts"] = {
			{
				["id"] = "1790700000-3-1790712000",
				["raid"] = "Цитадель Ледяной Короны",
				["size"] = 25,
				["heroic"] = true,
				["date"] = "29.09.2026",
				["from"] = 1790700000,
				["to"] = 1790712000,
				["busy"] = 9000,
				["tries"] = 3,
				["wipes"] = 1,
				["passed"] = 2,
				["known"] = 12,
				["players"] = 26,
				["dead"] = 9,
				["made"] = 1790712100,
				["bosses"] = {
					{
						["name"] = "Лорд Ребрад",
						["kill"] = true,
						["time"] = 105,
						["tries"] = 1,
						["wipes"] = 0,
						["deaths"] = 0,
						["dps"] = 301234,
						["rows"] = {
							{
								["name"] = "Тестмаг",
								["class"] = "MAGE",
								["spec"] = 2,
								["dps"] = 21000,
								["dmg"] = 2205000,
							}, -- [1]
							{
								["name"] = "Name \"quoted\" \\ back",
								["class"] = "ROGUE",
								["dps"] = 18000.5,
								["dmg"] = 1890000,
							}, -- [2]
						},
					}, -- [1]
					{
						["name"] = "Король-лич",
						["kill"] = false,
						["time"] = 412,
						["tries"] = 2,
						["wipes"] = 2,
						["deaths"] = 9,
						["dps"] = 250000,
						["rows"] = {
							{
								["name"] = "Танк",
								["class"] = "WARRIOR",
								["spec"] = 3,
								["dps"] = 9000,
								["dmg"] = 3708000,
							}, -- [1]
						},
					}, -- [2]
				},
				["deaths"] = {
					{
						["name"] = "Танк",
						["class"] = "WARRIOR",
						["n"] = 2,
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
		b.WriteString("ManaCodeRaidLeadDB = {\n\t[\"discord\"] = {\n\t\t[\"v\"] = 1,\n\t\t[\"posts\"] = {},\n\t},\n}\n")
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
		if p.ID != "1790700000-3-1790712000" || p.Raid != "Цитадель Ледяной Короны" || p.Size != 25 || !p.Heroic ||
			p.Known != 12 || p.Dead != 9 || p.Made != 1790712100 {
			t.Errorf("шапка: %+v", p)
		}
		if len(p.Bosses) != 2 || !p.Bosses[0].Kill || p.Bosses[1].Kill || p.Bosses[1].Wipes != 2 {
			t.Fatalf("боссы: %+v", p.Bosses)
		}
		r := p.Bosses[0].Rows
		if len(r) != 2 || r[0].Spec != 2 || r[1].Name != `Name "quoted" \ back` || r[1].DPS != 18000.5 {
			t.Errorf("строки: %+v", r)
		}
		if len(p.Deaths) != 1 || p.Deaths[0].N != 2 {
			t.Errorf("смерти: %+v", p.Deaths)
		}
	}
}

func TestReadDiscordMissing(t *testing.T) {
	src := []byte("\nManaCodeRaidHelperDB = {\n\t[\"recording\"] = true,\n}\nManaCodeRaidLeadDB = {\n\t[\"discord\"] = " + svPost + ",\n}\n")
	if _, err := readDiscord(src); err != errNoTable {
		t.Errorf("ключ чужой таблицы не наш: %v", err)
	}
	newer := strings.Replace(string(svFile(false, false)), "[\"v\"] = 1,", "[\"v\"] = 2,", 1)
	if _, err := readDiscord([]byte(newer)); err == nil {
		t.Error("новая версия таблицы — ошибка")
	}
	if _, err := readDiscord([]byte("\nManaCodeRaidHelperDB = {\n\t[\"discord\"] = {\n\t\t[\"v\"] = 1,\n")); err == nil {
		t.Error("оборванная таблица — ошибка")
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
	if out := os.Getenv("DISCORD_SAMPLE_PNG"); out != "" {
		src := svFile(false, false)
		if in := os.Getenv("DISCORD_SAMPLE_SV"); in != "" {
			if src, err = os.ReadFile(in); err != nil {
				t.Fatal(err)
			}
		}
		posts, err := readDiscord(src)
		if err != nil || len(posts) == 0 {
			t.Fatalf("образец %v", err)
		}
		pic, err := renderPost(posts[len(posts)-1])
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(out, pic, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("PNG: %s", out)
	}
}

type hookServer struct {
	mu     sync.Mutex
	calls  int
	status int
	got    []string
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
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			t.Errorf("не multipart: %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(r.FormValue("payload_json")), &payload); err != nil {
			t.Errorf("payload_json: %v", err)
		}
		h.got = append(h.got, payload["content"].(string))
		if f, fh, err := r.FormFile("files[0]"); err == nil {
			b, _ := io.ReadAll(f)
			h.pics = append(h.pics, b)
			if fh.Header.Get("Content-Type") != "image/png" || !bytes.HasPrefix(b, []byte("\x89PNG")) {
				t.Errorf("файл не PNG: %s", fh.Header.Get("Content-Type"))
			}
		} else {
			t.Errorf("нет files[0]: %v", err)
		}
		w.WriteHeader(h.status)
		if h.status == http.StatusTooManyRequests {
			_, _ = w.Write([]byte(`{"message":"You are being rate limited.","retry_after":1.5}`))
		}
	}))
	t.Cleanup(srv.Close)
	return h, srv
}

func TestPostWebhook(t *testing.T) {
	h, srv := newHook(t)
	p := samplePost(t)
	pic, _ := renderPost(p)
	if err := postWebhook(srv.URL, p, pic); err != nil {
		t.Fatal(err)
	}
	if h.calls != 1 || !strings.Contains(h.got[0], "**Цитадель Ледяной Короны 25 гер** — 29.09.2026") ||
		!strings.Contains(h.got[0], "убито 1 из 2") {
		t.Errorf("сообщение: %q", h.got)
	}
	h.status = http.StatusTooManyRequests
	if err := postWebhook(srv.URL, p, pic); err == nil || !strings.Contains(err.Error(), "подождать") {
		t.Errorf("429: %v", err)
	}
	h.status = http.StatusNotFound
	if err := postWebhook(srv.URL, p, pic); err == nil || !strings.Contains(err.Error(), "вебхук") {
		t.Errorf("404: %v", err)
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
	oldCfg, oldNow := userConfigDir, now
	userConfigDir = func() (string, error) { return cfg, nil }
	e := &discordEnv{addons: filepath.Join(root, "game", "Interface", "AddOns"), clock: time.Unix(1790712200, 0)}
	now = func() time.Time { return e.clock }
	t.Cleanup(func() { userConfigDir, now = oldCfg, oldNow })
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
	if !strings.HasPrefix(res[0].text(), "Discord: отправлен итог «Цитадель Ледяной Короны 25 гер, 29.09.2026»") {
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
