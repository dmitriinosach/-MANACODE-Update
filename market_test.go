package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func marketFixture(at int64) []byte {
	return []byte(fmt.Sprintf(`AuctionScanDB = {
 ["v"]=1, ["items"]={[36919]="Багровый рубин\t4"},
 ["scans"]={{["id"]="r|Horde|1|1",["t"]=%d,["secs"]=3,["realm"]="Реалм",
 ["faction"]="Horde",["scope"]="item",["itemID"]=36919,["mode"]="pages",["miss"]=0,
 ["lots"]={"36919:0:1:1:12345:0:4:Продавец"}}}}`, at))
}

func TestMarketLiteralParserCoverage(t *testing.T) {
	cfg := marketSettings{ServerID: "circle", Pool: "faction"}
	at := time.Now().Unix() - 60
	scans, items, err := readMarket(marketFixture(at), cfg)
	if err != nil {
		t.Fatal(err)
	}
	scan := scans[0]
	if scan.ObservedAt != at+3 || scan.Since != at || !scan.Complete || scan.Raw["itemID"] != float64(36919) {
		t.Fatalf("interval or coverage: %+v", scan)
	}
	if items["36919"] != "Багровый рубин\t4" {
		t.Fatal(items)
	}
	bad := append(marketFixture(at), []byte("; os.execute('anything')")...)
	if _, _, err := readMarket(bad, cfg); err == nil {
		t.Fatal("accepted executable Lua")
	}
	partial := strings.Replace(string(marketFixture(at)), `"item"`, `"disenchant"`, 1)
	scans, _, err = readMarket([]byte(partial), cfg)
	if err != nil || scans[0].Complete {
		t.Fatal("disenchant accepted as full market")
	}
}

func TestMarketSyncAcknowledgesAndInstalls(t *testing.T) {
	stubGame(t, false)
	home := t.TempDir()
	old := userConfigDir
	userConfigDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { userConfigDir = old })
	setupHome("")
	root := t.TempDir()
	addons := filepath.Join(root, "Interface", "AddOns")
	account := filepath.Join(root, "WTF", "Account", "TEST", "SavedVariables", marketName+".lua")
	if err := writeFile(account, marketFixture(time.Now().Unix()-60)); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(addons, marketName, marketName+".toc"), []byte("## Version: 0.1.0")); err != nil {
		t.Fatal(err)
	}
	posts := 0
	failed := true
	cursors := []string{}
	at := time.Now().Unix() - 30
	observations := []string{fmt.Sprintf(`{"sourceID":"other","scanID":"two","itemID":36919,"at":%d,"since":%d,"price":11111,"lots":2,"units":2,"complete":true,"clockStatus":"unverified"}`, at, at-1)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Error("missing token")
		}
		switch r.URL.Path {
		case "/v1/capabilities":
			fmt.Fprint(w, `{"v":1,"sourceID":"source-a","read":true,"write":true}`)
		case "/v1/scans":
			posts++
			if failed {
				w.WriteHeader(503)
				return
			}
			fmt.Fprint(w, `{"v":1,"acks":[{"sourceID":"source-a","scanID":"r|Horde|1|1","status":"duplicate"}]}`)
		case "/v1/snapshot":
			if r.URL.Query().Get("realm") != "Реалм" || r.URL.Query().Get("from") == "" {
				t.Error("market/window missing")
			}
			cursor := r.URL.Query().Get("cursor")
			cursors = append(cursors, cursor)
			done := 0
			if seq, _, ok := strings.Cut(cursor, ":"); ok {
				fmt.Sscan(seq, &done)
			}
			next := cursor
			if len(observations) > done {
				next = fmt.Sprintf("%d:36919", len(observations))
			}
			fmt.Fprintf(w, `{"v":1,"hasMore":false,"nextCursor":%q,"observations":[%s]}`, next, strings.Join(observations[done:], ","))
		default:
			t.Error(r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	shared := filepath.Join(addons, marketName, "data", "shared.lua")
	cfg := marketSettings{URL: srv.URL, Token: "token", ServerID: "circle", Realm: "Реалм", Faction: "Horde", Pool: "faction", Account: "TEST", Days: 7, Done: map[string]string{}}
	if _, err := marketSync(context.Background(), addons, &cfg, true, nil); err == nil || len(cfg.Done) != 0 {
		t.Fatal("failed upload acknowledged")
	}
	failed = false
	res, err := marketSync(context.Background(), addons, &cfg, true, nil)
	if err != nil || !res.Installed || res.Sent != 1 || res.Added != 1 {
		t.Fatal(res, err)
	}
	res, err = marketSync(context.Background(), addons, &cfg, false, nil)
	if err != nil || res.Pushed || res.Added != 0 || res.Total != 1 || res.Installed {
		t.Fatal(res, err)
	}
	if posts != 2 {
		t.Fatalf("expected failure then retry, no third POST: %d", posts)
	}
	if cursors[len(cursors)-1] != "1:36919" {
		t.Fatalf("history not incremental: %v", cursors)
	}
	installed, err := os.ReadFile(shared)
	if err != nil || !strings.Contains(string(installed), "price=11111") {
		t.Fatalf("inbox: %s, %v", installed, err)
	}
	if strings.Contains(string(installed), "token") {
		t.Fatal("token leaked into addon")
	}
	if st := loadMarketStatus(); st.Link != "ok" || st.Total != 1 || st.PushAt == 0 || st.InstalledAt == 0 {
		t.Fatalf("status: %+v", st)
	}
	stubGame(t, true)
	before := string(installed)
	observations = append(observations, fmt.Sprintf(`{"sourceID":"other","scanID":"three","itemID":36919,"at":%d,"since":%d,"price":22222,"lots":1,"units":1,"complete":true,"clockStatus":"unverified"}`, at+5, at+4))
	res, err = marketSync(context.Background(), addons, &cfg, false, nil)
	if err != nil || !res.Pending || res.Added != 1 || res.Total != 2 || !strings.Contains(res.note(), "закроете игру") {
		t.Fatal(res, err)
	}
	installed, _ = os.ReadFile(shared)
	if string(installed) != before {
		t.Fatal("inbox changed during game")
	}
	if !loadMarketStatus().Pending {
		t.Fatal("pending install not recorded")
	}
	stubGame(t, false)
	res, err = marketSync(context.Background(), addons, &cfg, false, nil)
	if err != nil || !res.Installed || res.Pending || res.Total != 2 {
		t.Fatal(res, err)
	}
	installed, _ = os.ReadFile(shared)
	if !strings.Contains(string(installed), "price=22222") || !strings.Contains(string(installed), "price=11111") {
		t.Fatalf("merged history not installed: %s", installed)
	}
	if err := writeFile(filepath.Join(addons, marketName, ".git", "HEAD"), []byte("ref")); err != nil {
		t.Fatal(err)
	}
	observations = append(observations, fmt.Sprintf(`{"sourceID":"other","scanID":"four","itemID":36919,"at":%d,"since":%d,"price":33333,"lots":1,"units":1,"complete":true,"clockStatus":"unverified"}`, at+9, at+8))
	before = string(installed)
	res, err = marketSync(context.Background(), addons, &cfg, false, nil)
	installed, _ = os.ReadFile(shared)
	if err != nil || !res.Dev || res.Pending || res.Installed || string(installed) != before || !loadMarketStatus().Dev {
		t.Fatalf("папка разработки — история в неё не пишется: %+v %v", res, err)
	}
}

func TestMarketLinkStates(t *testing.T) {
	home := t.TempDir()
	old := userConfigDir
	userConfigDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { userConfigDir = old })
	setupHome("")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	cfg := marketSettings{URL: srv.URL, Token: "expired"}
	if err := marketCheck(context.Background(), cfg); !errors.Is(err, errMarketDenied) || loadMarketStatus().Link != "denied" {
		t.Fatal("401 is not a denied link", err)
	}
	srv.Close()
	if err := marketCheck(context.Background(), cfg); !errors.Is(err, errMarketOffline) || loadMarketStatus().Link != "offline" {
		t.Fatal("closed server is not offline", err)
	}
}

func TestMarketInboxRejectsFutureAndEscapes(t *testing.T) {
	price := float64(12345)
	point := marketPoint{SourceID: `x"};error("no");--`, ScanID: "scan", ItemID: 36919,
		At: time.Now().Unix() - 10, Since: time.Now().Unix() - 20, Price: &price, Lots: 1, Units: 1, Complete: true, ClockStatus: "unverified"}
	future := point
	future.At = time.Now().Unix() + 3600
	partial := point
	partial.Complete = false
	inbox, err := marketLua(marketSettings{Realm: "Реалм", Faction: "Horde", Pool: "faction", ServerID: "circle"}, "local", []marketPoint{point, future, partial})
	if err != nil {
		t.Fatal(err)
	}
	head := strings.Index(string(inbox), "{v=1")
	p := &luaParser{s: inbox[head:]}
	v, err := p.value()
	if err != nil {
		t.Fatal(err)
	}
	table := v.(*luaTable)
	if len(table.table("observations").arr) != 1 || table.table("observations").list()[0].str("sourceID") != point.SourceID {
		t.Fatal("bad Lua quoting or partial/future filter")
	}
	for _, address := range []string{"http://example.com", "https://user:secret@example.com"} {
		_, err := marketRequest(context.Background(), marketSettings{URL: address}, http.MethodGet, "/v1/capabilities", nil)
		if err == nil {
			t.Fatal("unsafe endpoint accepted")
		}
	}
	if _, err := json.Marshal(point); err != nil {
		t.Fatal(err)
	}
}

func TestMarketLoginStoresOnlyProtectedToken(t *testing.T) {
	home := t.TempDir()
	old := userConfigDir
	userConfigDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { userConfigDir = old })
	setupHome("")
	var device string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/login" {
			t.Error(r.URL.Path)
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if request["login"] != "trader" || request["password"] != "private-password" {
			t.Error("wrong credentials DTO")
		}
		current, _ := request["deviceID"].(string)
		if len(current) != 64 {
			t.Error("no persistent device identity")
		}
		if device != "" && current != device {
			t.Error("changed source after repeated login")
		}
		device = current
		fmt.Fprint(w, `{"v":1,"token":"private-session-token","sourceID":"source-login","read":true,"write":true}`)
	}))
	defer srv.Close()
	cfg := marketSettings{URL: srv.URL, Account: "TEST", Done: map[string]string{}}
	for range 2 {
		if err := marketLogin(context.Background(), &cfg, "trader", "private-password"); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(marketPath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "private-password") || strings.Contains(string(data), "private-session-token") {
		t.Fatal("plaintext secret on disk")
	}
	loaded := loadMarket()
	if loaded.Token != "private-session-token" || loaded.Login != "trader" {
		t.Fatal("DPAPI session did not round-trip")
	}
}

func TestMarketRealmScans(t *testing.T) {
	cfg := &marketSettings{Realm: marketDefaultRealm}
	scans := []marketScan{
		{ScanID: "a", Realm: marketDefaultRealm, Faction: "Horde", ObservedAt: 1},
		{ScanID: "b", Realm: marketDefaultRealm, Faction: "Alliance", ObservedAt: 5},
		{ScanID: "c", Realm: "Другой реалм", Faction: "Horde", ObservedAt: 9},
	}
	kept := marketRealmScans(scans, cfg)
	if len(kept) != 2 || cfg.Faction != "Alliance" {
		t.Fatalf("kept=%d faction=%q", len(kept), cfg.Faction)
	}
	cfg = &marketSettings{Realm: marketDefaultRealm, Faction: "Horde"}
	marketRealmScans(scans, cfg)
	if cfg.Faction != "Horde" {
		t.Fatalf("заданная фракция затёрта: %q", cfg.Faction)
	}
}

func TestMarketPushBatches(t *testing.T) {
	home := t.TempDir()
	old := userConfigDir
	userConfigDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { userConfigDir = old })
	setupHome("")
	root := t.TempDir()
	addons := filepath.Join(root, "Interface", "AddOns")
	at := time.Now().Unix() - 600
	var rows []string
	for i := 1; i <= 120; i++ {
		rows = append(rows, fmt.Sprintf(`{["id"]="r|Horde|1|%d",["t"]=%d,["secs"]=3,["realm"]="Реалм",["faction"]="Horde",["scope"]="item",["itemID"]=36919,["mode"]="pages",["miss"]=0,["lots"]={"36919:0:1:1:%d:0:4:Продавец"}}`, i, at+int64(i), 1000+i))
	}
	sv := `AuctionScanDB = {["v"]=1,["items"]={[36919]="Багровый рубин\t4"},["scans"]={` + strings.Join(rows, ",") + `}}`
	if err := writeFile(filepath.Join(root, "WTF", "Account", "TEST", "SavedVariables", marketName+".lua"), []byte(sv)); err != nil {
		t.Fatal(err)
	}
	sizes := []int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Scans []marketScan `json:"scans"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		sizes = append(sizes, len(body.Scans))
		acks := []string{}
		for i, scan := range body.Scans {
			if len(sizes) == 2 && i == len(body.Scans)-1 {
				continue
			}
			acks = append(acks, fmt.Sprintf(`{"sourceID":"source-a","scanID":%q,"status":"inserted"}`, scan.ScanID))
		}
		fmt.Fprintf(w, `{"v":1,"acks":[%s]}`, strings.Join(acks, ","))
	}))
	defer srv.Close()
	cfg := marketSettings{URL: srv.URL, Token: "token", ServerID: "circle", Realm: "Реалм", Faction: "Horde", Pool: "faction", Account: "TEST", Done: map[string]string{}}
	steps := 0
	sent, err := marketPush(context.Background(), addons, &cfg, "source-a", func(int, int) { steps++ })
	if err == nil || sent != 99 || len(cfg.Done) != 99 || fmt.Sprint(sizes) != "[50 50]" {
		t.Fatalf("неподтверждённый скан пачки — ошибка, подтверждённые сохраняются: %d %v %v", sent, err, sizes)
	}
	sizes = nil
	sent, err = marketPush(context.Background(), addons, &cfg, "source-a", nil)
	if err != nil || sent != 21 || fmt.Sprint(sizes) != "[21]" || steps != 3 {
		t.Fatalf("повтор шлёт только оставшиеся одной пачкой: %d %v %v %d", sent, err, sizes, steps)
	}
}
