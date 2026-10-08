package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	discordFile       = "RaidHelperDiscord.json"
	discordDivider    = "RaidHelperDivider.png"
	discordLock       = "RaidHelperDiscord.lock"
	discordKeep       = 200
	discordRetry      = 5 * time.Minute
	discordPhase      = 30 * time.Second
	discordPicDir     = "RaidHelper"
	discordSent       = "sent"
	discordSaved      = "saved"
	discordFilesMax   = 10
	discordBatchBytes = 8 << 20
)

var reWebhook = regexp.MustCompile(`^https://(?:(?:ptb|canary)\.)?discord(?:app)?\.com/api/webhooks/\d+/[\w-]+$`)

var discordClient = &http.Client{Timeout: 30 * time.Second}

type discordState struct {
	Webhook string            `json:"webhook,omitempty"`
	Done    map[string]string `json:"done,omitempty"`
	Order   []string          `json:"order,omitempty"`
}

type discordResult struct {
	post discordPost
	sent bool
	file string
	pics int
	done int
	err  error
}

func (r discordResult) text() string {
	what := r.post.title()
	if d := r.post.date(); d != "" {
		what += ", " + d
	}
	switch {
	case r.err != nil:
		return "Discord: «" + what + "» не ушёл — " + r.err.Error()
	case r.sent:
		return "Discord: отправлен итог «" + what + "»"
	}
	if r.pics > 1 {
		return fmt.Sprintf("Discord не настроен — итог «%s» сохранён (%d картинок): %s", what, r.pics, r.file)
	}
	return "Discord не настроен — итог «" + what + "» сохранён: " + r.file
}

func discordPath() string {
	return homePath(discordFile)
}

func loadDiscord() discordState {
	var st discordState
	if p := discordPath(); p != "" {
		if b, err := os.ReadFile(p); err == nil {
			_ = json.Unmarshal(b, &st)
		}
	}
	if st.Done == nil {
		st.Done = map[string]string{}
	}
	return st
}

func saveDiscord(st discordState) error {
	p := discordPath()
	if p == "" {
		return errors.New("нет папки настроек пользователя")
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func (st *discordState) mark(id, how string) {
	if st.Done == nil {
		st.Done = map[string]string{}
	}
	if _, ok := st.Done[id]; !ok {
		st.Order = append(st.Order, id)
	}
	st.Done[id] = how
	for len(st.Order) > discordKeep {
		delete(st.Done, st.Order[0])
		st.Order = st.Order[1:]
	}
}

func validWebhook(url string) bool {
	return reWebhook.MatchString(strings.TrimSpace(url))
}

func setWebhook(url string) error {
	url = strings.TrimSpace(url)
	if url != "" && !validWebhook(url) {
		return errors.New("это не адрес вебхука Discord: нужен https://discord.com/api/webhooks/…")
	}
	unlock, err := lockDiscord()
	if err != nil {
		return err
	}
	defer unlock()
	st := loadDiscord()
	st.Webhook = url
	return saveDiscord(st)
}

func lockDiscord() (func(), error) {
	p := discordPath()
	if p == "" {
		return func() {}, nil
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	lp := filepath.Join(filepath.Dir(p), discordLock)
	for try := 0; try < 2; try++ {
		f, err := os.OpenFile(lp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			fmt.Fprintf(f, "pid=%d at=%s\n", os.Getpid(), now().UTC().Format(time.RFC3339))
			f.Close()
			return func() { _ = os.Remove(lp) }, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		if !lockExpired(lp) {
			return nil, errBusy
		}
		_ = os.Remove(lp)
	}
	return nil, errBusy
}

func picturesDir(addons string) string {
	return filepath.Join(gameDir(addons), "Screenshots", discordPicDir)
}

func picName(p discordPost, i, n int) string {
	name := strings.Map(func(r rune) rune {
		if r < '0' || r > '9' {
			return '-'
		}
		return r
	}, p.ID)
	base := "raid-" + strings.Trim(name, "-")
	if n > 1 {
		return fmt.Sprintf("%s-%d.png", base, i)
	}
	return base + ".png"
}

func discordMessage(p discordPost) string {
	head := "**" + p.title() + "**"
	if d := p.date(); d != "" {
		head += " — " + d
	}
	return head + "\n" + p.facts()
}

func picBatches(pics []discordPic) [][]discordPic {
	var out [][]discordPic
	var cur []discordPic
	size := 0
	for _, pic := range pics {
		if len(cur) > 0 && (len(cur) >= discordFilesMax || size+len(pic.Data) > discordBatchBytes) {
			out = append(out, cur)
			cur, size = nil, 0
		}
		cur = append(cur, pic)
		size += len(pic.Data)
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

// postWebhook шлёт картинки поста пачками; tail (разделитель гильдии) — последним отдельным сообщением.
func postWebhook(url string, p discordPost, pics, tail []discordPic, skip int) (int, error) {
	batches := picBatches(pics)
	if len(tail) > 0 {
		batches = append(batches, tail)
	}
	for i := skip; i < len(batches); i++ {
		content := ""
		if i == 0 {
			content = discordMessage(p)
		}
		if err := postBatch(url, content, batches[i]); err != nil {
			return i, err
		}
	}
	return len(batches), nil
}

// dividerPic — разделитель гильдии: картинка RaidHelperDivider.png рядом с настройками Discord
// (%APPDATA%\ManaCode). Есть — уходит в канал последним сообщением после итога; нет — ничего.
var dividerPic = func() []discordPic {
	file := homePath(discordDivider)
	if file == "" {
		return nil
	}
	data, err := os.ReadFile(file)
	if err != nil || len(data) == 0 {
		return nil
	}
	return []discordPic{{Name: "divider" + filepath.Ext(file), Data: data}}
}

func postBatch(url, content string, pics []discordPic) error {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	atts := make([]map[string]any, len(pics))
	for i, pic := range pics {
		atts[i] = map[string]any{"id": i, "filename": pic.Name}
	}
	msg := map[string]any{
		"allowed_mentions": map[string]any{"parse": []string{}},
		"attachments":      atts,
	}
	if content != "" {
		msg["content"] = content
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if err := mw.WriteField("payload_json", string(payload)); err != nil {
		return err
	}
	for i, pic := range pics {
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="files[%d]"; filename="%s"`, i, pic.Name))
		h.Set("Content-Type", "image/png")
		part, err := mw.CreatePart(h)
		if err != nil {
			return err
		}
		if _, err := part.Write(pic.Data); err != nil {
			return err
		}
	}
	if err := mw.Close(); err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, url, &body)
	if err != nil {
		return errors.New("адрес вебхука не читается")
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("User-Agent", userAgent)
	resp, err := discordClient.Do(req)
	if err != nil {
		return errors.New("Discord не ответил — нет сети?")
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode == http.StatusTooManyRequests:
		return errors.New("Discord просит подождать — повторю позже")
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusUnauthorized:
		return errors.New("вебхук не найден — проверьте адрес")
	case resp.StatusCode == http.StatusRequestEntityTooLarge:
		return errors.New("картинки тяжелее, чем принимает Discord")
	}
	var e struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(answer, &e) == nil && e.Message != "" {
		return fmt.Errorf("Discord ответил %d: %s", resp.StatusCode, e.Message)
	}
	return fmt.Errorf("Discord ответил %s", resp.Status)
}

func deliver(addons, hook string, p discordPost, skip int) discordResult {
	r := discordResult{post: p}
	pics, err := renderPics(addons, p)
	if err != nil {
		r.err = fmt.Errorf("картинка не нарисовалась: %w", err)
		return r
	}
	r.pics = len(pics)
	if hook != "" {
		r.done, r.err = postWebhook(hook, p, pics, dividerPic(), skip)
		r.sent = r.err == nil
		return r
	}
	folder := picturesDir(addons)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		r.err = fmt.Errorf("не смог создать папку %s", folder)
		return r
	}
	for i, pic := range pics {
		file := filepath.Join(folder, pic.Name)
		if i == 0 {
			r.file = file
		}
		if err := os.WriteFile(file, pic.Data, 0o644); err != nil {
			r.err = fmt.Errorf("не смог сохранить %s", file)
			return r
		}
	}
	return r
}

type svSeen struct {
	mod  time.Time
	size int64
}

type discordWatch struct {
	mu     sync.Mutex
	seen   map[string]svSeen
	posts  map[string][]discordPost
	failAt map[string]time.Time
	parts  map[string]int
}

func newDiscordWatch() *discordWatch {
	return &discordWatch{seen: map[string]svSeen{}, posts: map[string][]discordPost{}, failAt: map[string]time.Time{}, parts: map[string]int{}}
}

func svFiles(addons string) []string {
	var out []string
	for _, a := range listAccounts(accountsDir(filepath.Join(addons, raidHelperName)), config{}) {
		if a.exists {
			out = append(out, a.sv)
		}
	}
	return out
}

func (w *discordWatch) refresh(files []string) bool {
	changed := false
	for _, f := range files {
		st, err := os.Stat(f)
		if err != nil {
			continue
		}
		cur := svSeen{mod: st.ModTime(), size: st.Size()}
		if old, ok := w.seen[f]; ok && old == cur {
			continue
		}
		changed = true
		w.seen[f] = cur
		src, err := os.ReadFile(f)
		if err != nil {
			delete(w.seen, f)
			dcLog("%s не читается: %v", f, err)
			continue
		}
		posts, err := readDiscord(src)
		if err != nil {
			delete(w.posts, f)
			dcLog("%s: итоги не разобраны: %v", f, err)
			continue
		}
		w.posts[f] = posts
	}
	return changed
}

func (w *discordWatch) logState(why string, files []string, st discordState, queue int) {
	var list []string
	for _, f := range files {
		list = append(list, fmt.Sprintf("%s (итогов %d)", f, len(w.posts[f])))
	}
	sv := "не найдены"
	if len(list) > 0 {
		sv = strings.Join(list, "; ")
	}
	sent, saved := 0, 0
	for _, how := range st.Done {
		switch how {
		case discordSent:
			sent++
		case discordSaved:
			saved++
		}
	}
	browser := browserPath()
	dcLog("%s: SavedVariables: %s; в очереди %d, уже отправлено %d, сохранено файлом %d; вебхук %s; Edge/Chrome: %s",
		why, sv, queue, sent, saved, yesNo(st.Webhook != "", "задан", "не задан"), yesNo(browser != "", browser, "не найден"))
}

func logResult(r discordResult) {
	what := r.post.title()
	if d := r.post.date(); d != "" {
		what += ", " + d
	}
	switch {
	case r.err != nil:
		dcLog("«%s» [%s]: ошибка — %v (картинок %d, ушло частей %d)", what, r.post.ID, r.err, r.pics, r.done)
	case r.sent:
		dcLog("«%s» [%s]: отправлен (картинок %d)", what, r.post.ID, r.pics)
	default:
		dcLog("«%s» [%s]: сохранён в файл %s (картинок %d)", what, r.post.ID, r.file, r.pics)
	}
}

func (w *discordWatch) pending(st discordState) []discordPost {
	var out []discordPost
	seen := map[string]bool{}
	files := make([]string, 0, len(w.posts))
	for f := range w.posts {
		files = append(files, f)
	}
	sort.Strings(files)
	for _, f := range files {
		for _, p := range w.posts[f] {
			if seen[p.ID] || st.Done[p.ID] != "" {
				continue
			}
			if at, ok := w.failAt[p.ID]; ok && now().Sub(at) < discordRetry {
				continue
			}
			seen[p.ID] = true
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Finish < out[j].Finish })
	return out
}

func (w *discordWatch) latest() (discordPost, bool) {
	var best discordPost
	ok := false
	for _, list := range w.posts {
		for _, p := range list {
			if !ok || p.Finish > best.Finish {
				best, ok = p, true
			}
		}
	}
	return best, ok
}

func (w *discordWatch) poll(addons string) []discordResult {
	w.mu.Lock()
	defer w.mu.Unlock()
	files := svFiles(addons)
	changed := w.refresh(files)
	if len(w.pending(loadDiscord())) == 0 {
		if changed {
			w.logState("SavedVariables изменились, новых итогов нет", files, loadDiscord(), 0)
		}
		return nil
	}
	unlock, err := lockDiscord()
	if err != nil {
		dcLog("отправка отложена: %v", err)
		return nil
	}
	defer unlock()
	st := loadDiscord()
	list := w.pending(st)
	w.logState("попытка", files, st, len(list))
	var out []discordResult
	for _, p := range list {
		r := deliver(addons, st.Webhook, p, w.parts[p.ID])
		logResult(r)
		out = append(out, r)
		if r.err != nil {
			w.failAt[p.ID] = now()
			w.parts[p.ID] = r.done
			continue
		}
		delete(w.failAt, p.ID)
		delete(w.parts, p.ID)
		how := discordSaved
		if r.sent {
			how = discordSent
		}
		st.mark(p.ID, how)
		if err := saveDiscord(st); err != nil {
			out[len(out)-1].err = fmt.Errorf("отправлено, но не записал, что отправлено: %w", err)
		}
	}
	return out
}

func (w *discordWatch) again(addons string) (discordResult, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	files := svFiles(addons)
	w.refresh(files)
	p, ok := w.latest()
	if !ok {
		w.logState("«Отправить снова»: итогов нет", files, loadDiscord(), 0)
		return discordResult{}, false
	}
	unlock, err := lockDiscord()
	if err != nil {
		dcLog("«Отправить снова»: отправка уже идёт")
		return discordResult{post: p, err: errors.New("отправка уже идёт")}, true
	}
	defer unlock()
	st := loadDiscord()
	w.logState("«Отправить снова»", files, st, 1)
	r := deliver(addons, st.Webhook, p, 0)
	logResult(r)
	if r.err == nil {
		how := discordSaved
		if r.sent {
			how = discordSent
		}
		st.mark(p.ID, how)
		if err := saveDiscord(st); err != nil {
			r.err = err
		}
	}
	return r, true
}

func discordAuto(addons string, w *discordWatch) {
	for _, r := range w.poll(addons) {
		autoLog("%s", r.text())
	}
}
