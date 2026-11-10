package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	marketName      = "ManaCode_AuctionScan"
	marketInboxName = "ManaCode_AuctionScan_Shared"
	marketInboxVar  = "ManaCodeAuctionScanInbox"
	marketExchange  = 1
	marketOldestV   = 1
	marketNewestV   = 3
)

func marketScopeName(scope string) string {
	switch scope {
	case "gems":
		return "камни"
	case "item":
		return "предмет без itemID"
	}
	return scope
}

func marketSendable(scan marketScan) bool {
	switch scan.Scope {
	case "market", "query", "family", "disenchant":
		return true
	case "item":
		return scan.ItemID > 0
	}
	return false
}

var marketRepo = "dmitriinosach/-MANACODE-MarketBasedAuction"

// marketDefaultURL подставляется, пока в AuctionMarket.json не задан свой адрес.
const marketDefaultURL = "https://mcp.serverloc.crazedns.ru/market"

// Пока обрабатывается один рынок: Circle x1. Остальные реалмы не отправляются и не загружаются.
const (
	marketDefaultServer = "circle"
	marketDefaultRealm  = "WoW Circle 3.3.5a x1"
)

type marketSettings struct {
	URL       string            `json:"url"`
	Token     string            `json:"token"`
	Secret    string            `json:"protectedToken,omitempty"`
	Login     string            `json:"login,omitempty"`
	DeviceID  string            `json:"deviceID,omitempty"`
	ServerID  string            `json:"serverID"`
	Realm     string            `json:"realm"`
	Faction   string            `json:"faction"`
	Pool      string            `json:"pool"`
	Account   string            `json:"account"`
	Days      int               `json:"days"`
	Automatic bool              `json:"automatic"`
	Done      map[string]string `json:"done,omitempty"`
	Sources   map[string]string `json:"sources,omitempty"`
}

var (
	errMarketOffline = errors.New("сервер рынка не отвечает")
	errMarketDenied  = errors.New("база рынка отклонила доступ")
	errMarketRunning = errors.New("обмен аукциона уже запущен")
	errMarketBase    = errors.New("сканы не отправлены")
)

var marketSyncMutex = `Local\Manacode_MarketSync`

var marketHTTP = func() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{Timeout: 8 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	transport.TLSHandshakeTimeout = 8 * time.Second
	transport.ResponseHeaderTimeout = 90 * time.Second
	return &http.Client{Transport: transport, Timeout: 3 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
}()

const (
	marketBatchScans = 50
	marketBatchBytes = 6 << 20
	marketBatchItems = 10000
)

const (
	marketPullEvery = 2 * time.Minute
	marketFullEvery = 24 * time.Hour
	marketLinkEvery = time.Minute
)

type marketStatus struct {
	Link        string `json:"link"`
	LinkNote    string `json:"linkNote,omitempty"`
	LinkAt      int64  `json:"linkAt,omitempty"`
	PushAt      int64  `json:"pushAt,omitempty"`
	Sent        int    `json:"sent"`
	Held        string `json:"held,omitempty"`
	Deferred    int    `json:"deferred,omitempty"`
	DeferredOf  string `json:"deferredOf,omitempty"`
	PullAt      int64  `json:"pullAt,omitempty"`
	Added       int    `json:"added"`
	Total       int    `json:"total"`
	Pending     bool   `json:"pending"`
	InstalledAt int64  `json:"installedAt,omitempty"`
}

type marketHistory struct {
	Key    string        `json:"key"`
	Cursor string        `json:"cursor"`
	Full   int64         `json:"full"`
	Points []marketPoint `json:"points"`
}

type marketResult struct {
	Linked     bool
	Pushed     bool
	Sent       int
	Held       string
	Deferred   int
	DeferredOf string
	Added      int
	Total      int
	Pending    bool
	Installed  bool
}

func marketDeferredNote(n int, of string) string {
	return fmt.Sprintf("отложено %s %s (%s): сервер их пока не принимает", num(n), ruPlural(n, "скан", "скана", "сканов"), of)
}

func (r marketResult) note() string {
	parts := []string{}
	if r.Pushed {
		parts = append(parts, fmt.Sprintf("Отправлено сканов: %d.", r.Sent))
	}
	if r.Held != "" {
		parts = append(parts, "Сканы не отправлены: "+r.Held+".")
	}
	if r.Deferred > 0 {
		parts = append(parts, "Сканы: "+marketDeferredNote(r.Deferred, r.DeferredOf)+".")
	}
	parts = append(parts, fmt.Sprintf("История: %s %s, новых %s.", num(r.Total), ruPlural(r.Total, "наблюдение", "наблюдения", "наблюдений"), num(r.Added)))
	switch {
	case r.Pending:
		parts = append(parts, "Встанет в аддон, когда закроете игру.")
	case r.Installed:
		parts = append(parts, "Записана в аддон: подхватится при следующем запуске игры.")
	}
	return strings.Join(parts, " ")
}

type marketScan struct {
	ScanID     string         `json:"scanID"`
	ObservedAt int64          `json:"observedAt"`
	Since      int64          `json:"since"`
	ServerID   string         `json:"serverID"`
	Realm      string         `json:"realm"`
	Faction    string         `json:"faction"`
	Pool       string         `json:"pool"`
	Scope      string         `json:"scope"`
	ItemID     int32          `json:"itemID,omitempty"`
	Family     string         `json:"family,omitempty"`
	Query      string         `json:"query,omitempty"`
	Filter     map[string]any `json:"filter"`
	Complete   bool           `json:"complete"`
	Truncated  bool           `json:"truncated"`
	Mode       string         `json:"mode"`
	Secs       int            `json:"secs"`
	Expect     int            `json:"expect"`
	Miss       int            `json:"miss"`
	Lots       []string       `json:"lots"`
	Raw        map[string]any `json:"raw"`
}

type marketPoint struct {
	SourceID    string   `json:"sourceID"`
	ScanID      string   `json:"scanID"`
	ItemID      int32    `json:"itemID"`
	At          int64    `json:"at"`
	Since       int64    `json:"since"`
	Price       *float64 `json:"price"`
	P25         *float64 `json:"p25"`
	Lots        int      `json:"lots"`
	Units       int64    `json:"units"`
	Complete    bool     `json:"complete"`
	Scope       string   `json:"scope"`
	ClockStatus string   `json:"clockStatus"`
	ReceivedAt  int64    `json:"receivedAt"`
}

type marketReply struct {
	V            int           `json:"v"`
	SourceID     string        `json:"sourceID"`
	Token        string        `json:"token"`
	Read         bool          `json:"read"`
	Write        bool          `json:"write"`
	Observations []marketPoint `json:"observations"`
	NextCursor   string        `json:"nextCursor"`
	HasMore      bool          `json:"hasMore"`
	ServerAt     int64         `json:"serverAt"`
	Acks         []struct {
		SourceID string `json:"sourceID"`
		ScanID   string `json:"scanID"`
		Status   string `json:"status"`
	} `json:"acks"`
}

func marketPath() string { return homePath("AuctionMarket.json") }

func marketStatusPath() string { return homePath("AuctionMarket-status.json") }

func marketHistoryPath() string { return homePath("AuctionMarket-history.json") }

func loadMarketStatus() marketStatus {
	var st marketStatus
	if b, err := os.ReadFile(marketStatusPath()); err == nil {
		_ = json.Unmarshal(b, &st)
	}
	return st
}

func saveMarketJSON(p string, v any) error {
	if p == "" {
		return errors.New("нет папки настроек пользователя")
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(p+".tmp", b, 0o600); err != nil {
		return err
	}
	return os.Rename(p+".tmp", p)
}

func updateMarketStatus(change func(*marketStatus)) {
	st := loadMarketStatus()
	change(&st)
	_ = saveMarketJSON(marketStatusPath(), st)
}

func marketLinkState(err error) (string, string) {
	switch {
	case err == nil:
		return "ok", ""
	case errors.Is(err, errMarketDenied):
		return "denied", err.Error()
	case errors.Is(err, errMarketOffline):
		return "offline", err.Error()
	}
	return "error", err.Error()
}

func markMarketLink(err error) {
	link, note := marketLinkState(err)
	updateMarketStatus(func(st *marketStatus) {
		st.Link, st.LinkNote, st.LinkAt = link, note, time.Now().Unix()
	})
}

func marketCheck(ctx context.Context, cfg marketSettings) error {
	reply, err := marketRequest(ctx, cfg, http.MethodGet, "/v1/capabilities", nil)
	if err == nil && (!reply.Read || !reply.Write || reply.SourceID == "") {
		err = fmt.Errorf("%w: нет прав обмена", errMarketDenied)
	}
	markMarketLink(err)
	return err
}

func marketNewestAccount(addons string) string {
	best, at := "", time.Time{}
	for _, path := range marketFiles(addons) {
		if st, err := os.Stat(path); err == nil && st.ModTime().After(at) {
			best, at = filepath.Base(filepath.Dir(filepath.Dir(path))), st.ModTime()
		}
	}
	return best
}

func marketHistoryKey(cfg marketSettings, source string) string {
	return strings.Join([]string{cfg.URL, source, cfg.ServerID, cfg.Realm, cfg.Faction, cfg.Pool, strconv.Itoa(cfg.Days)}, "|")
}

func loadMarketHistory() marketHistory {
	var h marketHistory
	if b, err := os.ReadFile(marketHistoryPath()); err == nil {
		_ = json.Unmarshal(b, &h)
	}
	return h
}

func loadMarket() marketSettings {
	cfg := marketSettings{Days: 30, Pool: "faction", Done: map[string]string{}}
	b, err := os.ReadFile(marketPath())
	if err == nil {
		_ = json.Unmarshal(b, &cfg)
	}
	if cfg.URL == "" {
		cfg.URL = marketDefaultURL
	}
	if cfg.ServerID == "" {
		cfg.ServerID = marketDefaultServer
	}
	if cfg.Realm == "" {
		cfg.Realm = marketDefaultRealm
	}
	if cfg.Done == nil {
		cfg.Done = map[string]string{}
	}
	if cfg.Sources == nil {
		cfg.Sources = map[string]string{}
	}
	if cfg.Secret != "" && cfg.Token == "" {
		cfg.Token, _ = openMarketToken(cfg.Secret)
	}
	return cfg
}

func saveMarket(cfg marketSettings) error {
	if cfg.Token != "" {
		secret, err := sealMarketToken(cfg.Token)
		if err != nil {
			return err
		}
		cfg.Secret, cfg.Token = secret, ""
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	p := marketPath()
	if p == "" {
		return errors.New("нет папки настроек пользователя")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(p+".tmp", b, 0o600); err != nil {
		return err
	}
	return os.Rename(p+".tmp", p)
}

func marketLogin(ctx context.Context, cfg *marketSettings, login, password string) error {
	if cfg.Account == "" {
		return errors.New("сначала выберите аккаунт со сканами")
	}
	if login == "" || password == "" {
		return errors.New("введите логин и пароль от базы данных рынка")
	}
	if cfg.DeviceID == "" {
		var bytes [32]byte
		if _, err := rand.Read(bytes[:]); err != nil {
			return err
		}
		cfg.DeviceID = hex.EncodeToString(bytes[:])
		if err := saveMarket(*cfg); err != nil {
			return err
		}
	}
	device := sha256.Sum256([]byte(cfg.DeviceID + "\x00" + cfg.Account))
	body := map[string]any{"v": 1, "login": login, "password": password, "deviceID": hex.EncodeToString(device[:])}
	reply, err := marketRequest(ctx, *cfg, http.MethodPost, "/v1/login", body)
	if errors.Is(err, errMarketDenied) {
		err = fmt.Errorf("%w: неверный логин или пароль БД, либо доступ отключён", errMarketDenied)
	}
	markMarketLink(err)
	if err != nil {
		return err
	}
	if reply.Token == "" || reply.SourceID == "" || !reply.Read || !reply.Write {
		return errors.New("сервер не выдал доступ")
	}
	cfg.Login, cfg.Token = login, reply.Token
	return saveMarket(*cfg)
}

func marketFiles(addons string) []string {
	root := filepath.Join(filepath.Dir(filepath.Dir(addons)), "WTF", "Account")
	files, _ := filepath.Glob(filepath.Join(root, "*", "SavedVariables", marketName+".lua"))
	sort.Strings(files)
	return files
}

func marketValue(v any) any {
	t, ok := v.(*luaTable)
	if !ok {
		return v
	}
	if len(t.m) == 0 {
		arr := make([]any, 0, len(t.arr))
		for _, entry := range t.arr {
			arr = append(arr, marketValue(entry))
		}
		return arr
	}
	result := map[string]any{}
	for key, value := range t.m {
		result[key] = marketValue(value)
	}
	for i, value := range t.arr {
		result[strconv.Itoa(i+1)] = marketValue(value)
	}
	return result
}

type marketBase struct {
	scans  []marketScan
	items  map[string]string
	broken int
}

func marketContract(db *luaTable) error {
	if raw := db.get("exchange"); raw != nil {
		ex, ok := raw.(float64)
		switch {
		case ok && ex > marketExchange && ex == float64(int64(ex)):
			return fmt.Errorf("%w: база Market Based Auction новее клиента синхронизации (обмен %d, клиент понимает %d) — обновите клиент синхронизации", errMarketBase, int64(ex), marketExchange)
		case !ok || ex < 1 || ex != float64(int64(ex)):
			return fmt.Errorf("%w: неверный номер обмена в базе Market Based Auction", errMarketBase)
		}
		return nil
	}
	v := db.num("v")
	switch {
	case v >= marketOldestV && v <= marketNewestV && v == float64(int64(v)):
		return nil
	case v > marketNewestV:
		return fmt.Errorf("%w: база Market Based Auction версии %s без номера обмена — обновите клиент синхронизации", errMarketBase, strconv.FormatFloat(v, 'f', -1, 64))
	}
	return fmt.Errorf("%w: у базы Market Based Auction нет номера версии — зайдите в игру с аддоном, он её обновит", errMarketBase)
}

func readMarket(raw []byte, cfg marketSettings) (marketBase, error) {
	p := &luaParser{s: raw}
	p.ws()
	const head = "AuctionScanDB"
	if !bytes.HasPrefix(raw[p.i:], []byte(head)) {
		return marketBase{}, fmt.Errorf("%w: в SavedVariables нет базы Market Based Auction", errMarketBase)
	}
	p.i += len(head)
	p.ws()
	if p.i >= len(raw) || raw[p.i] != '=' {
		return marketBase{}, fmt.Errorf("%w: файл базы Market Based Auction повреждён", errMarketBase)
	}
	p.i++
	v, err := p.value()
	p.ws()
	db, ok := v.(*luaTable)
	if err != nil || !ok || p.i != len(raw) {
		return marketBase{}, fmt.Errorf("%w: файл базы Market Based Auction повреждён или дописывается игрой — повторите после выхода из игры", errMarketBase)
	}
	if err := marketContract(db); err != nil {
		return marketBase{}, err
	}
	base := marketBase{items: map[string]string{}, scans: []marketScan{}}
	if table := db.table("items"); table != nil {
		for key, value := range table.m {
			if name, ok := value.(string); ok {
				base.items[key] = name
			}
		}
	}
	for _, row := range db.table("scans").list() {
		scan, ok := marketRow(row, cfg)
		if !ok {
			base.broken++
			continue
		}
		base.scans = append(base.scans, scan)
	}
	return base, nil
}

func marketRow(row *luaTable, cfg marketSettings) (marketScan, bool) {
	if row.str("id") == "" || row.num("t") <= 0 {
		return marketScan{}, false
	}
	lots := []string{}
	if table := row.table("lots"); table != nil {
		for _, entry := range table.arr {
			text, ok := entry.(string)
			if !ok {
				return marketScan{}, false
			}
			lots = append(lots, text)
		}
	}
	rawRow, ok := marketValue(row).(map[string]any)
	if !ok {
		return marketScan{}, false
	}
	scope := row.str("scope")
	if scope == "" {
		scope = "market"
	}
	complete := row.num("miss") == 0 && !row.flag("truncated") && (scope == "market" || scope == "item" || scope == "family")
	if flag, exists := row.get("complete").(bool); exists {
		complete = complete && flag
	}
	if row.str("mode") == "all" && row.num("expect") > float64(len(lots)) {
		complete = false
	}
	filter := map[string]any{}
	for _, key := range []string{"classIndex", "subClassIndex", "minLevel", "maxLevel", "minQuality", "deGroup", "deOptions", "expectedItems", "observedItems", "coverage"} {
		if value := row.get(key); value != nil {
			filter[key] = marketValue(value)
		}
	}
	return marketScan{
		ScanID: row.str("id"), Since: int64(row.num("t")),
		ObservedAt: int64(row.num("t") + row.num("secs")),
		ServerID:   cfg.ServerID, Realm: row.str("realm"), Faction: row.str("faction"), Pool: cfg.Pool,
		Scope: scope, ItemID: int32(row.num("itemID")), Family: row.text("family"), Query: row.str("query"),
		Filter: filter, Complete: complete, Truncated: row.flag("truncated"), Mode: row.str("mode"),
		Secs: int(row.num("secs")), Expect: int(row.num("expect")), Miss: int(row.num("miss")),
		Lots: lots, Raw: rawRow,
	}, true
}

func marketRequest(ctx context.Context, cfg marketSettings, method, path string, body any) (marketReply, error) {
	u, err := url.Parse(cfg.URL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return marketReply{}, errors.New("укажите адрес сервиса в AuctionMarket.json")
	}
	local := u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"
	if u.Scheme != "https" && !(u.Scheme == "http" && local) {
		return marketReply{}, errors.New("сервису нужен HTTPS; HTTP допустим только на localhost")
	}
	b, err := json.Marshal(body)
	if err != nil {
		return marketReply{}, err
	}
	if len(b) > 8<<20 {
		return marketReply{}, errors.New("скан больше 8 МиБ: нужен отдельный импорт")
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(cfg.URL, "/")+path, bytes.NewReader(b))
	if err != nil {
		return marketReply{}, errors.New("неверный адрес сервиса")
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := marketHTTP.Do(req)
	if err != nil {
		return marketReply{}, fmt.Errorf("%w: нет связи с сервером, проверьте интернет", errMarketOffline)
	}
	defer resp.Body.Close()
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		return marketReply{}, fmt.Errorf("%w: вместо API пришла веб-страница, проверьте адрес и вход на роутере", errMarketOffline)
	}
	if resp.StatusCode != http.StatusOK {
		switch {
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			return marketReply{}, fmt.Errorf("%w: доступ истёк или отозван (HTTP %d)", errMarketDenied, resp.StatusCode)
		case resp.StatusCode == http.StatusTooManyRequests:
			return marketReply{}, errors.New("слишком много попыток подключения; повторите позднее (HTTP 429)")
		case resp.StatusCode == http.StatusServiceUnavailable:
			return marketReply{}, fmt.Errorf("%w: база данных на сервере недоступна (HTTP 503)", errMarketOffline)
		case resp.StatusCode >= 500:
			return marketReply{}, fmt.Errorf("%w: HTTP %d", errMarketOffline, resp.StatusCode)
		}
		return marketReply{}, fmt.Errorf("сервис обмена: HTTP %d", resp.StatusCode)
	}
	limited := io.LimitReader(resp.Body, (4<<20)+1)
	data, err := io.ReadAll(limited)
	if err != nil || len(data) > 4<<20 {
		return marketReply{}, errors.New("слишком большой или оборванный ответ сервиса")
	}
	var reply marketReply
	if err := json.Unmarshal(data, &reply); err != nil || reply.V != 1 {
		return marketReply{}, errors.New("неподдерживаемый ответ сервиса")
	}
	return reply, nil
}

func marketSync(ctx context.Context, addons string, cfg *marketSettings, push bool, progress func(sent, total int)) (marketResult, error) {
	var res marketResult
	unlockSync, ok := lockWatch(marketSyncMutex)
	if !ok {
		return res, errMarketRunning
	}
	defer unlockSync()
	if cfg.Done == nil {
		cfg.Done = map[string]string{}
	}
	capabilities, err := marketRequest(ctx, *cfg, http.MethodGet, "/v1/capabilities", nil)
	if err == nil && (!capabilities.Read || !capabilities.Write || capabilities.SourceID == "") {
		err = fmt.Errorf("%w: нет прав обмена", errMarketDenied)
	}
	markMarketLink(err)
	if err != nil {
		return res, err
	}
	res.Linked = true
	if cfg.Sources == nil {
		cfg.Sources = map[string]string{}
	}
	binding := cfg.URL + "|" + capabilities.SourceID
	account, bound := cfg.Sources[binding]
	if bound && account != cfg.Account {
		return res, errors.New("этому токену уже привязан другой аккаунт; для второго аккаунта нужен отдельный токен источника")
	}
	cfg.Sources[binding] = cfg.Account
	faction := cfg.Faction
	if push || cfg.Faction == "" {
		push, err := marketPush(ctx, addons, cfg, capabilities.SourceID, progress)
		switch {
		case errors.Is(err, errMarketBase):
			res.Held = strings.TrimPrefix(err.Error(), errMarketBase.Error()+": ")
		case err != nil:
			return res, err
		default:
			res.Pushed, res.Sent, res.Deferred, res.DeferredOf = true, push.sent, push.deferred, push.deferredOf
		}
		updateMarketStatus(func(st *marketStatus) {
			st.PushAt, st.Sent, st.Held, st.Deferred, st.DeferredOf = time.Now().Unix(), res.Sent, res.Held, res.Deferred, res.DeferredOf
		})
	}
	if !bound || faction != cfg.Faction {
		if err := saveMarket(*cfg); err != nil {
			return res, err
		}
	}
	h, added, err := marketPull(ctx, *cfg, capabilities.SourceID, capabilities.ServerAt)
	if err != nil {
		return res, err
	}
	res.Added, res.Total = added, len(h.Points)
	res.Pending, res.Installed, err = marketInstall(addons, *cfg, capabilities.SourceID, h)
	updateMarketStatus(func(st *marketStatus) {
		st.PullAt, st.Added, st.Total, st.Pending = time.Now().Unix(), added, len(h.Points), res.Pending
		if res.Installed {
			st.InstalledAt = time.Now().Unix()
		}
	})
	return res, err
}

type marketQueued struct {
	scan   marketScan
	key    string
	digest string
	size   int
	items  map[string]string
}

type marketPushed struct {
	sent       int
	deferred   int
	deferredOf string
}

func marketPush(ctx context.Context, addons string, cfg *marketSettings, source string, progress func(sent, total int)) (marketPushed, error) {
	var out marketPushed
	var path string
	for _, candidate := range marketFiles(addons) {
		if filepath.Base(filepath.Dir(filepath.Dir(candidate))) == cfg.Account {
			path = candidate
		}
	}
	if path == "" {
		return out, fmt.Errorf("%w: выберите аккаунт со сканами", errMarketBase)
	}
	st, err := os.Stat(path)
	if err != nil || st.Size() > 512<<20 {
		return out, fmt.Errorf("%w: файл базы Market Based Auction недоступен или больше 512 МиБ", errMarketBase)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return out, fmt.Errorf("%w: файл базы Market Based Auction не читается", errMarketBase)
	}
	after, err := os.Stat(path)
	if err != nil || st.Size() != after.Size() || !st.ModTime().Equal(after.ModTime()) {
		return out, fmt.Errorf("%w: игра записывает сканы, повторите позже", errMarketBase)
	}
	base, err := readMarket(raw, *cfg)
	if err != nil {
		return out, err
	}
	scans := marketRealmScans(base.scans, cfg)
	queue := []marketQueued{}
	held := map[string]bool{}
	for _, scan := range scans {
		if !marketSendable(scan) {
			out.deferred++
			held[marketScopeName(scan.Scope)] = true
			continue
		}
		content, err := json.Marshal(scan)
		if err != nil {
			return out, err
		}
		hash := sha256.Sum256(content)
		q := marketQueued{scan: scan, key: cfg.URL + "|" + source + "|" + scan.ScanID, digest: hex.EncodeToString(hash[:]), size: len(content), items: map[string]string{}}
		if cfg.Done[q.key] == q.digest {
			continue
		}
		for _, lot := range scan.Lots {
			id, _, _ := strings.Cut(lot, ":")
			if name, exists := base.items[id]; exists {
				q.items[id] = name
				q.size += len(id) + len(name) + 8
			}
		}
		if q.size > marketBatchBytes || len(q.items) > marketBatchItems {
			continue
		}
		queue = append(queue, q)
	}
	if base.broken > 0 {
		out.deferred += base.broken
		held["повреждённые"] = true
	}
	kinds := make([]string, 0, len(held))
	for kind := range held {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	out.deferredOf = strings.Join(kinds, ", ")
	out.sent, err = marketSend(ctx, cfg, source, queue, progress)
	return out, err
}

func marketSend(ctx context.Context, cfg *marketSettings, source string, queue []marketQueued, progress func(sent, total int)) (int, error) {
	sent := 0
	if progress != nil {
		progress(0, len(queue))
	}
	for len(queue) > 0 {
		n, size, names := 0, 0, map[string]string{}
		for n < len(queue) && n < marketBatchScans {
			extra := 0
			for id := range queue[n].items {
				if _, exists := names[id]; !exists {
					extra++
				}
			}
			if n > 0 && (size+queue[n].size > marketBatchBytes || len(names)+extra > marketBatchItems) {
				break
			}
			for id, name := range queue[n].items {
				names[id] = name
			}
			size += queue[n].size
			n++
		}
		batch := make([]marketScan, n)
		for i := range batch {
			batch[i] = queue[i].scan
		}
		body := map[string]any{"v": 1, "sourceID": source, "scans": batch, "items": names}
		reply, err := marketRequest(ctx, *cfg, http.MethodPost, "/v1/scans", body)
		acked := map[string]bool{}
		for _, ack := range reply.Acks {
			if ack.SourceID == source && (ack.Status == "inserted" || ack.Status == "duplicate") {
				acked[ack.ScanID] = true
			}
		}
		done := 0
		for _, q := range queue[:n] {
			if acked[q.scan.ScanID] {
				cfg.Done[q.key] = q.digest
				done++
			}
		}
		if done > 0 {
			if err := saveMarket(*cfg); err != nil {
				return sent, err
			}
		}
		sent += done
		if progress != nil {
			progress(sent, sent+len(queue)-n)
		}
		if err != nil {
			return sent, err
		}
		if done < n {
			return sent, fmt.Errorf("сервер подтвердил %d из %d сканов", done, n)
		}
		queue = queue[n:]
	}
	return sent, nil
}

func marketPointKey(p marketPoint) string {
	return p.SourceID + "\x00" + p.ScanID + "\x00" + strconv.Itoa(int(p.ItemID))
}

func marketPull(ctx context.Context, cfg marketSettings, source string, serverAt int64) (marketHistory, int, error) {
	if cfg.ServerID == "" || cfg.Realm == "" || cfg.Faction == "" || cfg.Pool == "" {
		return marketHistory{}, 0, errors.New("задайте serverID, realm, faction и pool в настройках обмена")
	}
	now := time.Now().UTC().Unix()
	to := now
	if serverAt > 0 {
		to = serverAt
	}
	from := int64(1)
	if cfg.Days > 0 {
		from = to - int64(cfg.Days)*86400
	}
	key := marketHistoryKey(cfg, source)
	h := loadMarketHistory()
	known := map[string]bool{}
	if h.Key == key {
		for _, p := range h.Points {
			known[marketPointKey(p)] = true
		}
	}
	if h.Key != key || now-h.Full > int64(marketFullEvery/time.Second) {
		h = marketHistory{Key: key}
	}
	fresh, cursor, err := marketDownload(ctx, cfg, from, to, h.Cursor)
	if err != nil && h.Cursor != "" && !errors.Is(err, errMarketOffline) && !errors.Is(err, errMarketDenied) {
		h = marketHistory{Key: key}
		fresh, cursor, err = marketDownload(ctx, cfg, from, to, "")
	}
	if err != nil {
		return h, 0, err
	}
	if h.Cursor == "" {
		h.Full = now
	}
	index := map[string]int{}
	for i, p := range h.Points {
		index[marketPointKey(p)] = i
	}
	added := 0
	for _, p := range fresh {
		k := marketPointKey(p)
		if i, exists := index[k]; exists {
			h.Points[i] = p
			continue
		}
		index[k] = len(h.Points)
		h.Points = append(h.Points, p)
		if !known[k] {
			added++
		}
	}
	kept := h.Points[:0]
	for _, p := range h.Points {
		if p.At >= from {
			kept = append(kept, p)
		}
	}
	h.Points = kept
	if len(h.Points) > 100000 {
		return h, 0, errors.New("история больше 100000 наблюдений: выберите меньший период")
	}
	sort.Slice(h.Points, func(i, j int) bool {
		if h.Points[i].At != h.Points[j].At {
			return h.Points[i].At < h.Points[j].At
		}
		return marketPointKey(h.Points[i]) < marketPointKey(h.Points[j])
	})
	h.Cursor = cursor
	return h, added, saveMarketJSON(marketHistoryPath(), h)
}

func marketDownload(ctx context.Context, cfg marketSettings, from, to int64, cursor string) ([]marketPoint, string, error) {
	query := url.Values{"serverID": {cfg.ServerID}, "realm": {cfg.Realm}, "faction": {cfg.Faction},
		"pool": {cfg.Pool}, "from": {strconv.FormatInt(from, 10)}, "to": {strconv.FormatInt(to, 10)}, "limit": {"500"}}
	points, seen := []marketPoint{}, map[string]bool{}
	for {
		query.Set("cursor", cursor)
		reply, err := marketRequest(ctx, cfg, http.MethodGet, "/v1/snapshot?"+query.Encode(), nil)
		if err != nil {
			return nil, "", err
		}
		points = append(points, reply.Observations...)
		if len(points) > 100000 {
			return nil, "", errors.New("история больше 100000 наблюдений: выберите меньший период")
		}
		if !reply.HasMore {
			if reply.NextCursor != "" {
				cursor = reply.NextCursor
			}
			return points, cursor, nil
		}
		if reply.NextCursor == "" || seen[reply.NextCursor] {
			return nil, "", errors.New("сервер повторяет страницу истории")
		}
		cursor = reply.NextCursor
		seen[cursor] = true
	}
}

type marketFile struct {
	path string
	data []byte
}

const marketInboxPart = 40000

const marketInboxToc = `## Interface: 30300
## Title: |cff3399ff[MANACODE]|r AuctionScan Shared
## Notes: Shared market history for ManaCode AuctionScan, written by ManaCode Update.
## Notes-ruRU: Общая история рынка для ManaCode AuctionScan, её пишет ManaCode Update
## Author: Manacode
## LoadOnDemand: 1
## X-ManaCode-Inbox: 1

`

func marketInstall(addons string, cfg marketSettings, source string, h marketHistory) (bool, bool, error) {
	inbox, err := marketTable(cfg, source, h.Points)
	if err != nil {
		return false, false, err
	}
	fallback := inbox.newest(marketInboxPart)
	shared := marketLua("local _, ns = ...\nns.SharedInbox", fallback, fallback.obs)
	stash := homePath("AuctionMarket-inbox.lua")
	if old, err := os.ReadFile(stash); err != nil || !bytes.Equal(old, shared) {
		if err := writeFile(stash, shared); err != nil {
			return false, false, err
		}
	}
	dir := folderIn(addons, marketName)
	if _, err := os.Stat(filepath.Join(dir, marketName+".toc")); err != nil {
		return false, false, errors.New("сначала установите Market Based Auction")
	}
	shelf := folderIn(addons, marketInboxName)
	files := marketInboxFiles(inbox)
	for i := range files {
		files[i].path = filepath.Join(shelf, files[i].path)
	}
	if !devFolder(dir) {
		files = append(files, marketFile{filepath.Join(dir, "data", "shared.lua"), shared})
	}
	keep := map[string]bool{}
	stale := []marketFile{}
	for _, f := range files {
		keep[strings.ToLower(f.path)] = true
		if old, err := os.ReadFile(f.path); err != nil || !bytes.Equal(old, f.data) {
			stale = append(stale, f)
		}
	}
	extra, _ := filepath.Glob(filepath.Join(shelf, "inbox_*.lua"))
	for _, path := range extra {
		if !keep[strings.ToLower(path)] {
			stale = append(stale, marketFile{path: path})
		}
	}
	if len(stale) == 0 {
		return false, false, nil
	}
	if gameClosed() != nil {
		return true, false, nil
	}
	unlock, err := lockInstall(addons)
	if err != nil {
		return true, false, err
	}
	defer unlock()
	if gameClosed() != nil {
		return true, false, nil
	}
	for _, f := range stale {
		if f.data == nil {
			if err := os.Remove(f.path); err != nil && !os.IsNotExist(err) {
				return true, false, err
			}
			continue
		}
		if err := writeFile(f.path, f.data); err != nil {
			return true, false, err
		}
	}
	return false, true, nil
}

func marketInboxFiles(inbox marketInbox) []marketFile {
	first := inbox.obs[:min(len(inbox.obs), marketInboxPart)]
	files := []marketFile{{"", nil}, {"inbox.lua", marketLua(marketInboxVar, inbox, first)}}
	toc := marketInboxToc + "inbox.lua\n"
	for i, at := 2, len(first); at < len(inbox.obs); i, at = i+1, at+marketInboxPart {
		name := fmt.Sprintf("inbox_%d.lua", i)
		toc += name + "\n"
		data := "local inbox = " + marketInboxVar + "\ninbox.parts = inbox.parts or {}\ninbox.parts[#inbox.parts + 1] = {\n" +
			strings.Join(inbox.obs[at:min(len(inbox.obs), at+marketInboxPart)], "") + "}\n"
		files = append(files, marketFile{name, []byte(data)})
	}
	files[0] = marketFile{marketInboxName + ".toc", []byte(toc)}
	return files
}

func marketQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, c := range []byte(s) {
		switch {
		case c == '"' || c == '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		case c < 32 || c == 127:
			fmt.Fprintf(&b, "\\%03d", c)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}

type marketInbox struct {
	market  string
	obs     []string
	version string
}

func marketVersion(market string, obs []string) string {
	sum := sha256.New()
	sum.Write([]byte(market))
	for _, o := range obs {
		sum.Write([]byte(o))
	}
	return hex.EncodeToString(sum.Sum(nil))
}

func (in marketInbox) newest(n int) marketInbox {
	if len(in.obs) <= n {
		return in
	}
	in.obs = in.obs[len(in.obs)-n:]
	in.version = marketVersion(in.market, in.obs)
	return in
}

func marketLua(name string, inbox marketInbox, obs []string) []byte {
	return []byte(name + " = {v=1,version=" + marketQuote(inbox.version) + "," + inbox.market + "observations={\n" + strings.Join(obs, "") + "}}\n")
}

func marketTable(cfg marketSettings, source string, points []marketPoint) (marketInbox, error) {
	var b strings.Builder
	b.WriteString("market={")
	for _, pair := range [][2]string{{"serverID", cfg.ServerID}, {"realm", cfg.Realm}, {"faction", cfg.Faction}, {"pool", cfg.Pool}} {
		fmt.Fprintf(&b, "%s=%s,", pair[0], marketQuote(pair[1]))
	}
	fmt.Fprintf(&b, "},localSourceID=%s,", marketQuote(source))
	inbox := marketInbox{market: b.String(), obs: []string{}}
	for _, p := range points {
		badIdentity := p.SourceID == "" || len(p.SourceID) > 128 || p.ScanID == "" || len(p.ScanID) > 256 || p.ItemID <= 0
		badTime := p.At <= 0 || p.At > time.Now().Unix() || p.Since <= 0 || p.Since > p.At
		badCounts := p.Lots < 0 || p.Lots > 10000000 || p.Units < 0 || p.Units > 1000000000
		validClock := p.ClockStatus == "verified" || p.ClockStatus == "unverified"
		if badIdentity || badTime || badCounts || !p.Complete || !validClock {
			continue
		}
		var o strings.Builder
		fmt.Fprintf(&o, "{sourceID=%s,scanID=%s,itemID=%d,at=%d,since=%d,lots=%d,units=%d,complete=true,clockStatus=%s,",
			marketQuote(p.SourceID), marketQuote(p.ScanID), p.ItemID, p.At, p.Since, p.Lots, p.Units, marketQuote(p.ClockStatus))
		for _, price := range []struct {
			key   string
			value *float64
		}{{key: "price", value: p.Price}, {key: "p25", value: p.P25}} {
			if price.value == nil {
				continue
			}
			if !(*price.value >= 0 && *price.value <= 1e12) {
				return marketInbox{}, errors.New("неверная цена в истории")
			}
			fmt.Fprintf(&o, "%s=%s,", price.key, strconv.FormatFloat(*price.value, 'f', -1, 64))
		}
		o.WriteString("},\n")
		inbox.obs = append(inbox.obs, o.String())
	}
	inbox.version = marketVersion(inbox.market, inbox.obs)
	return inbox, nil
}

// marketRealmScans оставляет сканы рынка cfg.Realm. Пустую фракцию настроек
// берёт у самого свежего скана: аукционы фракций раздельные.
func marketRealmScans(scans []marketScan, cfg *marketSettings) []marketScan {
	kept := make([]marketScan, 0, len(scans))
	var newest int64 = -1
	faction := ""
	for _, scan := range scans {
		if scan.Realm != cfg.Realm {
			continue
		}
		kept = append(kept, scan)
		if scan.Faction != "" && scan.ObservedAt >= newest {
			newest, faction = scan.ObservedAt, scan.Faction
		}
	}
	if cfg.Faction == "" {
		cfg.Faction = faction
	}
	return kept
}
