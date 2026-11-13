package main

import (
	"bytes"
	"context"
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
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	discordFile       = "RaidHelperDiscord.json"
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

var discordClient = &http.Client{}

var discordSleep = time.Sleep

const (
	discordBaseTimeout = 60 * time.Second
	discordMinRate     = 64 << 10
	discordWaitInline  = 10 * time.Second
	discordWaitTries   = 2
	discordWaitMax     = time.Hour
)

func discordTimeout(size int) time.Duration {
	return discordBaseTimeout + time.Duration(size/discordMinRate)*time.Second
}

var errTooLarge = errors.New("картинки тяжелее, чем принимает Discord")

var errPageTooLarge = errors.New("страница тяжелее, чем принимает Discord")

const webNote = "Рейд целиком: скачать и открыть в браузере"

type discordState struct {
	Webhook string                     `json:"webhook,omitempty"`
	Done    map[string]string          `json:"done,omitempty"`
	Order   []string                   `json:"order,omitempty"`
	Parts   map[string]discordProgress `json:"parts,omitempty"`
}

type discordProgress struct {
	Done    int       `json:"done,omitempty"`
	Via     string    `json:"via,omitempty"`
	Batches int       `json:"batches,omitempty"`
	Next    time.Time `json:"next,omitempty"`
}

type discordWait struct {
	after time.Duration
}

func (e *discordWait) Error() string {
	return "Discord просит подождать — повторю позже"
}

type discordStatus struct {
	code int
	text string
}

func (e *discordStatus) Error() string {
	return e.text
}

type discordResult struct {
	post   discordPost
	sent   bool
	file   string
	pics   int
	done   int
	html   int
	wait   time.Duration
	noPage error
	err    error
}

func (r discordResult) text() string {
	what := r.post.title()
	if d := r.post.date(); d != "" {
		what += ", " + d
	}
	switch {
	case r.err != nil:
		return fmt.Sprintf(tr("Discord: «%s» не ушёл — %s"), what, tr(r.err.Error()))
	case r.sent && r.noPage != nil:
		return fmt.Sprintf(tr("Discord: отправлен итог «%s» без выгрузки: %s"), what, tr(r.noPage.Error()))
	case r.sent && r.html > 0:
		return fmt.Sprintf(tr("Discord: отправлен итог «%s» с выгрузкой рейда (%s)"), what, mb(int64(r.html)))
	case r.sent:
		return fmt.Sprintf(tr("Discord: отправлен итог «%s»"), what)
	}
	if r.pics > 1 {
		return fmt.Sprintf(tr("Discord не настроен — итог «%s» сохранён (%d картинок): %s"), what, r.pics, r.file)
	}
	return fmt.Sprintf(tr("Discord не настроен — итог «%s» сохранён: %s"), what, r.file)
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
	delete(st.Parts, id)
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
	return updateDiscord(func(st *discordState) { st.Webhook = url })
}

func updateDiscord(change func(*discordState)) error {
	unlock, err := lockDiscord()
	if err != nil {
		return err
	}
	defer unlock()
	st := loadDiscord()
	change(&st)
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

func postWebhook(url string, p discordPost, pics, page []discordPic, skip int, sent func(int)) (int, error, error) {
	batches := picBatches(pics)
	pics0 := len(batches)
	if len(page) > 0 {
		batches = append(batches, page)
	}
	for i := skip; i < len(batches); i++ {
		content := ""
		switch {
		case i == 0:
			content = discordMessage(p)
		case i >= pics0:
			content = tr(webNote)
		}
		err := postBatchWait(url, content, batches[i])
		if err != nil && i >= pics0 && pageRejected(err) {
			if errors.Is(err, errTooLarge) {
				err = errPageTooLarge
			}
			dcLog("«%s»: страницу Discord не принял — %v; пост без неё", p.title(), err)
			return len(batches), err, nil
		}
		if err != nil {
			return i, nil, err
		}
		if sent != nil {
			sent(i + 1)
		}
	}
	return len(batches), nil, nil
}

func pageRejected(err error) bool {
	if errors.Is(err, errTooLarge) {
		return true
	}
	var st *discordStatus
	return errors.As(err, &st) && st.code == http.StatusBadRequest
}

func postBatchWait(url, content string, pics []discordPic) error {
	for try := 0; ; try++ {
		err := postBatch(url, content, pics)
		var w *discordWait
		if !errors.As(err, &w) || try >= discordWaitTries || w.after > discordWaitInline {
			return err
		}
		discordSleep(w.after)
	}
}

func retryAfter(h http.Header, body []byte) time.Duration {
	var d time.Duration
	var e struct {
		RetryAfter float64 `json:"retry_after"`
	}
	if json.Unmarshal(body, &e) == nil && e.RetryAfter > 0 {
		d = time.Duration(e.RetryAfter * float64(time.Second))
	}
	if s, err := strconv.ParseFloat(strings.TrimSpace(h.Get("Retry-After")), 64); err == nil && s > 0 {
		d = max(d, time.Duration(s*float64(time.Second)))
	}
	return min(max(d, time.Second), discordWaitMax)
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
		h.Set("Content-Type", attachType(pic.Name))
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
	ctx, cancel := context.WithTimeout(context.Background(), discordTimeout(body.Len()))
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &body)
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
		return &discordWait{after: retryAfter(resp.Header, answer)}
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusUnauthorized:
		return errors.New("вебхук не найден — проверьте адрес")
	case resp.StatusCode == http.StatusRequestEntityTooLarge:
		return errTooLarge
	}
	var e struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(answer, &e) == nil && e.Message != "" {
		return &discordStatus{code: resp.StatusCode, text: fmt.Sprintf("Discord ответил %d: %s", resp.StatusCode, e.Message)}
	}
	return &discordStatus{code: resp.StatusCode, text: fmt.Sprintf("Discord ответил %s", resp.Status)}
}

func resumeAt(old discordProgress, via string, batches int) int {
	switch {
	case old.Done == 0:
		return 0
	case old.Via == via && old.Batches == batches:
		return old.Done
	}
	return batches
}

func deliver(addons string, st *discordState, p discordPost, resume bool) discordResult {
	hook := st.Webhook
	key := p.key()
	r := discordResult{post: p}
	var old discordProgress
	if resume && hook != "" {
		old = st.Parts[key]
	}
	pics, via, err := renderPics(addons, p, old.Via)
	if err != nil {
		r.err = fmt.Errorf("картинка не нарисовалась: %w", err)
		return r
	}
	r.pics = len(pics)
	page, perr := raidPage(addons, p)
	r.noPage = perr
	if len(page) > 0 {
		r.html = len(page[0].Data)
	}
	if hook != "" {
		batches := len(picBatches(pics))
		skip := resumeAt(old, via, batches)
		if skip != old.Done {
			dcLog("«%s» [%s]: картинки нарисованы иначе (%s, пачек %d; было %s, %d, ушло %d) — повторно их не шлю",
				p.title(), key, via, batches, old.Via, old.Batches, old.Done)
		}
		saved := func(done int) {
			if st.Parts == nil {
				st.Parts = map[string]discordProgress{}
			}
			st.Parts[key] = discordProgress{Done: done, Via: via, Batches: batches}
			if err := saveDiscord(*st); err != nil {
				dcLog("«%s» [%s]: не записал, сколько ушло: %v", p.title(), key, err)
			}
		}
		var pageErr error
		r.done, pageErr, r.err = postWebhook(hook, p, pics, page, skip, saved)
		if pageErr != nil {
			r.noPage = pageErr
		}
		var w *discordWait
		if errors.As(r.err, &w) {
			r.wait = w.after
		}
		r.sent = r.err == nil
		return r
	}
	folder := picturesDir(addons)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		r.err = fmt.Errorf("не смог создать папку %s", folder)
		return r
	}
	for i, pic := range append(pics, page...) {
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
	mu    sync.Mutex
	seen  map[string]svSeen
	posts map[string][]discordPost
}

func newDiscordWatch() *discordWatch {
	return &discordWatch{seen: map[string]svSeen{}, posts: map[string][]discordPost{}}
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
	if gameFileAccess.check(true) != nil {
		return false
	}
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
		src, err := readSavedVariables(f)
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
		dcLog("«%s» [%s]: ошибка — %v (картинок %d, ушло частей %d)", what, r.post.key(), r.err, r.pics, r.done)
	case r.sent && r.noPage != nil:
		dcLog("«%s» [%s]: отправлен без выгрузки — %v (картинок %d)", what, r.post.key(), r.noPage, r.pics)
	case r.sent:
		dcLog("«%s» [%s]: отправлен (картинок %d, выгрузка %d байт)", what, r.post.key(), r.pics, r.html)
	default:
		dcLog("«%s» [%s]: сохранён в файл %s (картинок %d)", what, r.post.key(), r.file, r.pics)
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
			if seen[p.key()] || st.Done[p.key()] != "" {
				continue
			}
			if next := st.Parts[p.key()].Next; !next.IsZero() && now().Before(next) {
				continue
			}
			seen[p.key()] = true
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
		r := deliver(addons, &st, p, true)
		logResult(r)
		out = append(out, r)
		if r.err != nil {
			if st.Parts == nil {
				st.Parts = map[string]discordProgress{}
			}
			pr := st.Parts[p.key()]
			pr.Next = now().Add(max(discordRetry, r.wait))
			st.Parts[p.key()] = pr
			if err := saveDiscord(st); err != nil {
				dcLog("«%s» [%s]: не записал время повтора: %v", p.title(), p.key(), err)
			}
			continue
		}
		how := discordSaved
		if r.sent {
			how = discordSent
		}
		st.mark(p.key(), how)
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
	r := deliver(addons, &st, p, false)
	logResult(r)
	if r.err == nil {
		how := discordSaved
		if r.sent {
			how = discordSent
		}
		st.mark(p.key(), how)
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

func attachType(name string) string {
	if strings.EqualFold(filepath.Ext(name), ".html") {
		return "text/html; charset=utf-8"
	}
	return "image/png"
}

var raidHTML = buildRaidHTML

func raidPage(addons string, p discordPost) ([]discordPic, error) {
	start := now()
	ex, err := raidHTML(addons, p, "")
	if err != nil {
		dcLog("«%s»: выгрузка рейда не собрана — %v (%s)", p.title(), err, ex.Parts)
		return nil, err
	}
	dcLog("«%s»: выгрузка %s, %d байт за %v; %s", p.title(), ex.Name, len(ex.HTML), now().Sub(start).Round(time.Millisecond), ex.Parts)
	return []discordPic{{Name: ex.Name, Data: ex.HTML}}, nil
}
