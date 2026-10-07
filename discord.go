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
	discordFile   = "RaidHelperDiscord.json"
	discordLock   = "RaidHelperDiscord.lock"
	discordKeep   = 200
	discordRetry  = 5 * time.Minute
	discordPhase  = 30 * time.Second
	discordPicDir = "RaidHelper"
	discordSent   = "sent"
	discordSaved  = "saved"
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
	err  error
}

func (r discordResult) text() string {
	what := r.post.title()
	if r.post.Date != "" {
		what += ", " + r.post.Date
	}
	switch {
	case r.err != nil:
		return "Discord: «" + what + "» не ушёл — " + r.err.Error()
	case r.sent:
		return "Discord: отправлен итог «" + what + "»"
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

func picName(p discordPost) string {
	name := strings.Map(func(r rune) rune {
		if r < '0' || r > '9' {
			return '-'
		}
		return r
	}, p.ID)
	return "raid-" + strings.Trim(name, "-") + ".png"
}

func discordMessage(p discordPost) string {
	head := "**" + p.title() + "**"
	if p.Date != "" {
		head += " — " + p.Date
	}
	return head + "\n" + p.facts()
}

func postWebhook(url string, p discordPost, pic []byte) error {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	name := picName(p)
	payload, err := json.Marshal(map[string]any{
		"content":          discordMessage(p),
		"allowed_mentions": map[string]any{"parse": []string{}},
		"attachments":      []map[string]any{{"id": 0, "filename": name}},
	})
	if err != nil {
		return err
	}
	if err := mw.WriteField("payload_json", string(payload)); err != nil {
		return err
	}
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="files[0]"; filename="`+name+`"`)
	h.Set("Content-Type", "image/png")
	part, err := mw.CreatePart(h)
	if err != nil {
		return err
	}
	if _, err := part.Write(pic); err != nil {
		return err
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
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode == http.StatusTooManyRequests:
		return errors.New("Discord просит подождать — повторю позже")
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusUnauthorized:
		return errors.New("вебхук не найден — проверьте адрес")
	}
	var e struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(msg, &e) == nil && e.Message != "" {
		return fmt.Errorf("Discord ответил %d: %s", resp.StatusCode, e.Message)
	}
	return fmt.Errorf("Discord ответил %s", resp.Status)
}

func deliver(addons, hook string, p discordPost) discordResult {
	r := discordResult{post: p}
	pic, err := renderPost(p)
	if err != nil {
		r.err = fmt.Errorf("картинка не нарисовалась: %w", err)
		return r
	}
	if hook != "" {
		r.err = postWebhook(hook, p, pic)
		r.sent = r.err == nil
		return r
	}
	folder := picturesDir(addons)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		r.err = fmt.Errorf("не смог создать папку %s", folder)
		return r
	}
	r.file = filepath.Join(folder, picName(p))
	if err := os.WriteFile(r.file, pic, 0o644); err != nil {
		r.err = fmt.Errorf("не смог сохранить %s", r.file)
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
}

func newDiscordWatch() *discordWatch {
	return &discordWatch{seen: map[string]svSeen{}, posts: map[string][]discordPost{}, failAt: map[string]time.Time{}}
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

func (w *discordWatch) refresh(files []string) {
	for _, f := range files {
		st, err := os.Stat(f)
		if err != nil {
			continue
		}
		cur := svSeen{mod: st.ModTime(), size: st.Size()}
		if old, ok := w.seen[f]; ok && old == cur {
			continue
		}
		w.seen[f] = cur
		src, err := os.ReadFile(f)
		if err != nil {
			delete(w.seen, f)
			continue
		}
		posts, err := readDiscord(src)
		if err != nil {
			delete(w.posts, f)
			continue
		}
		w.posts[f] = posts
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
	sort.SliceStable(out, func(i, j int) bool { return out[i].Made < out[j].Made })
	return out
}

func (w *discordWatch) latest() (discordPost, bool) {
	var best discordPost
	ok := false
	for _, list := range w.posts {
		for _, p := range list {
			if !ok || p.Made > best.Made {
				best, ok = p, true
			}
		}
	}
	return best, ok
}

func (w *discordWatch) poll(addons string) []discordResult {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.refresh(svFiles(addons))
	if len(w.pending(loadDiscord())) == 0 {
		return nil
	}
	unlock, err := lockDiscord()
	if err != nil {
		return nil
	}
	defer unlock()
	st := loadDiscord()
	var out []discordResult
	for _, p := range w.pending(st) {
		r := deliver(addons, st.Webhook, p)
		out = append(out, r)
		if r.err != nil {
			w.failAt[p.ID] = now()
			continue
		}
		delete(w.failAt, p.ID)
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
	w.refresh(svFiles(addons))
	p, ok := w.latest()
	if !ok {
		return discordResult{}, false
	}
	unlock, err := lockDiscord()
	if err != nil {
		return discordResult{post: p, err: errors.New("отправка уже идёт")}, true
	}
	defer unlock()
	st := loadDiscord()
	r := deliver(addons, st.Webhook, p)
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
		autoLog("", "%s", r.text())
	}
}
