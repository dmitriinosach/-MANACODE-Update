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
	"sort"
	"strings"
	"testing"
	"time"
)

func marketHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	old := userConfigDir
	userConfigDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { userConfigDir = old })
	setupHome("")
	return home
}

func marketGame(t *testing.T, sv []byte) string {
	t.Helper()
	root := t.TempDir()
	addons := filepath.Join(root, "Interface", "AddOns")
	if err := writeFile(filepath.Join(root, "WTF", "Account", "TEST", "SavedVariables", marketName+".lua"), sv); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(addons, marketName, marketName+".toc"), []byte("## Version: 0.1.0")); err != nil {
		t.Fatal(err)
	}
	return addons
}

func TestMarketFixtureEveryScope(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "auctionscan", "AuctionScanDB.lua"))
	if err != nil {
		t.Fatal(err)
	}
	var want struct {
		Exchange int `json:"exchange"`
		Scans    []struct {
			ID        string `json:"id"`
			T         int64  `json:"t"`
			Realm     string `json:"realm"`
			Faction   string `json:"faction"`
			Mode      string `json:"mode"`
			Scope     string `json:"scope"`
			ItemID    int32  `json:"itemID"`
			Family    int    `json:"family"`
			Query     string `json:"query"`
			Truncated bool   `json:"truncated"`
			Secs      int    `json:"secs"`
			Expect    int    `json:"expect"`
			Miss      int    `json:"miss"`
			Lots      int    `json:"lots"`
		} `json:"scans"`
		Items []struct {
			ID      int    `json:"id"`
			Name    string `json:"name"`
			Quality int    `json:"quality"`
		} `json:"items"`
	}
	b, err := os.ReadFile(filepath.Join("testdata", "auctionscan", "AuctionScanDB.expected.json"))
	if err != nil || json.Unmarshal(b, &want) != nil {
		t.Fatal("образец .expected.json не читается", err)
	}
	if want.Exchange > marketExchange {
		t.Fatalf("образец аддона на обмене %d, клиент понимает %d: поднять marketExchange вместе с разбором", want.Exchange, marketExchange)
	}
	cfg := marketSettings{ServerID: "circle", Pool: "faction"}
	base, err := readMarket(raw, cfg)
	if err != nil || base.broken != 0 || len(base.scans) != len(want.Scans) {
		t.Fatalf("образец со всеми scope: %v, битых %d, сканов %d из %d", err, base.broken, len(base.scans), len(want.Scans))
	}
	for i, w := range want.Scans {
		got := base.scans[i]
		scope := w.Scope
		if scope == "" {
			scope = "market"
		}
		family := ""
		if w.Family != 0 {
			family = fmt.Sprint(w.Family)
		}
		if got.ScanID != w.ID || got.Since != w.T || got.ObservedAt != w.T+int64(w.Secs) || got.Realm != w.Realm || got.Faction != w.Faction ||
			got.Mode != w.Mode || got.Scope != scope || got.ItemID != w.ItemID || got.Family != family || got.Query != w.Query ||
			got.Truncated != w.Truncated || got.Expect != w.Expect || got.Miss != w.Miss || len(got.Lots) != w.Lots {
			t.Errorf("скан %d: %+v, ждали %+v", i+1, got, w)
		}
		if got.Complete && (scope != "market" && scope != "item" && scope != "family" || w.Miss > 0 || w.Truncated) {
			t.Errorf("скан %d (%s) помечен полным", i+1, scope)
		}
	}
	for _, w := range want.Items {
		if base.items[fmt.Sprint(w.ID)] != fmt.Sprintf("%s\t%d", w.Name, w.Quality) {
			t.Errorf("предмет %d: %q", w.ID, base.items[fmt.Sprint(w.ID)])
		}
	}

	marketHome(t)
	addons := marketGame(t, raw)
	scopes := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Scans []marketScan `json:"scans"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		acks := []string{}
		for _, scan := range body.Scans {
			scopes = append(scopes, scan.Scope)
			acks = append(acks, fmt.Sprintf(`{"sourceID":"source-a","scanID":%q,"status":"inserted"}`, scan.ScanID))
		}
		fmt.Fprintf(w, `{"v":1,"acks":[%s]}`, strings.Join(acks, ","))
	}))
	defer srv.Close()
	cfg = marketSettings{URL: srv.URL, Token: "token", ServerID: "circle", Realm: "WoW Circle 3.3.5a x1", Faction: "Alliance", Pool: "faction", Account: "TEST", Done: map[string]string{}}
	push, err := marketPush(context.Background(), addons, &cfg, "source-a", nil)
	sort.Strings(scopes)
	if err != nil || push.sent != 6 || push.deferred != 1 || push.deferredOf != "камни" || strings.Join(scopes, ",") != "disenchant,family,item,market,market,query" {
		t.Fatalf("камни откладываются, остальные уходят: %+v %v %v", push, err, scopes)
	}
}

func TestMarketContract(t *testing.T) {
	scan := `{["id"]="r|Horde|1|1",["t"]=1792900000,["secs"]=3,["realm"]="Реалм",["faction"]="Horde",["mode"]="pages",["miss"]=0,["lots"]={"36919:0:1:1:12345:0:4:Продавец"}}`
	for _, c := range []struct {
		head string
		ok   bool
		says string
	}{
		{`["v"]=1`, true, ""},
		{`["v"]=3`, true, ""},
		{`["v"]=99,["exchange"]=1`, true, ""},
		{`["v"]=3,["exchange"]=2`, false, "обновите клиент синхронизации"},
		{`["v"]=4`, false, "обновите клиент синхронизации"},
		{`["seq"]=1`, false, "нет номера версии"},
		{`["v"]=3,["exchange"]="1"`, false, "неверный номер обмена"},
		{`["v"]=3,["exchange"]=0`, false, "неверный номер обмена"},
	} {
		sv := `AuctionScanDB = {` + c.head + `,["future"]={["x"]=1},["scans"]={` + scan + `}}`
		base, err := readMarket([]byte(sv), marketSettings{})
		if c.ok != (err == nil) || c.ok && len(base.scans) != 1 {
			t.Errorf("%s: %v", c.head, err)
		}
		if !c.ok && (!errors.Is(err, errMarketBase) || !strings.Contains(err.Error(), c.says)) {
			t.Errorf("%s: причина %q, ждали %q", c.head, err, c.says)
		}
	}
	odd := strings.Replace(scan, `["mode"]`, `["scope"]="future",["newField"]={1,2},["mode"]`, 1)
	broken := strings.Replace(strings.Replace(scan, `|1"`, `|2"`, 1), `{"36919`, `{12,"36919`, 1)
	sv := `AuctionScanDB = {["v"]=3,["exchange"]=1,["scans"]={` + strings.Join([]string{scan, odd, broken}, ",") + `}}`
	base, err := readMarket([]byte(sv), marketSettings{})
	if err != nil || len(base.scans) != 2 || base.broken != 1 || base.scans[1].Scope != "future" || base.scans[1].Complete || marketSendable(base.scans[1]) {
		t.Fatalf("незнакомый scope читается и не уходит, битый скан не валит базу: %+v %v", base, err)
	}
}

func TestMarketNewerBaseStillInstallsHistory(t *testing.T) {
	stubGame(t, false)
	marketHome(t)
	addons := marketGame(t, []byte(`AuctionScanDB = {["v"]=4,["exchange"]=2,["scans"]={}}`))
	at := time.Now().Unix() - 30
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/capabilities":
			fmt.Fprint(w, `{"v":1,"sourceID":"source-a","read":true,"write":true}`)
		case "/v1/snapshot":
			fmt.Fprintf(w, `{"v":1,"hasMore":false,"nextCursor":"1:1","observations":[{"sourceID":"other","scanID":"two","itemID":36919,"at":%d,"since":%d,"price":11111,"lots":2,"units":2,"complete":true,"clockStatus":"unverified"}]}`, at, at-1)
		default:
			t.Error("сканы новой базы ушли на сервер:", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer srv.Close()
	cfg := marketSettings{URL: srv.URL, Token: "token", ServerID: "circle", Realm: "Реалм", Faction: "Horde", Pool: "faction", Account: "TEST", Days: 7, Done: map[string]string{}}
	res, err := marketSync(context.Background(), addons, &cfg, true, nil)
	if err != nil || res.Pushed || !res.Installed || !strings.Contains(res.Held, "обновите клиент синхронизации") || strings.HasPrefix(res.Held, errMarketBase.Error()) {
		t.Fatalf("отказ только для отправки, история ставится: %+v %v", res, err)
	}
	if !strings.Contains(res.note(), "Сканы не отправлены: база Market Based Auction новее") || !strings.Contains(loadMarketStatus().Held, "обновите клиент") {
		t.Fatalf("причина в окне: %q %+v", res.note(), loadMarketStatus())
	}
}

func TestMarketInboxParts(t *testing.T) {
	stubGame(t, false)
	marketHome(t)
	addons := marketGame(t, nil)
	at := time.Now().Unix() - 3600
	points := make([]marketPoint, marketInboxPart+5)
	for i := range points {
		points[i] = marketPoint{SourceID: "other", ScanID: fmt.Sprint("s", i), ItemID: 36919, At: at, Since: at - 1, Lots: 1, Units: 1, Complete: true, ClockStatus: "verified"}
	}
	cfg := marketSettings{ServerID: "circle", Realm: "Реалм", Faction: "Horde", Pool: "faction"}
	shelf := filepath.Join(addons, marketInboxName)
	if _, installed, err := marketInstall(addons, cfg, "self", marketHistory{Points: points}); err != nil || !installed {
		t.Fatal(installed, err)
	}
	toc, _ := os.ReadFile(filepath.Join(shelf, marketInboxName+".toc"))
	part, err := os.ReadFile(filepath.Join(shelf, "inbox_2.lua"))
	if !strings.HasSuffix(string(toc), "\ninbox.lua\ninbox_2.lua\n") || !strings.Contains(string(toc), "## LoadOnDemand: 1") || err != nil ||
		strings.Count(string(part), "{sourceID=") != 5 || !strings.HasPrefix(string(part), "local inbox = "+marketInboxVar+"\n") {
		t.Fatalf("toc %q, часть %v", toc, err)
	}
	first, _ := os.ReadFile(filepath.Join(shelf, "inbox.lua"))
	if strings.Count(string(first), "{sourceID=") != marketInboxPart {
		t.Fatal("в первом файле не 40 000 наблюдений")
	}
	shared, _ := os.ReadFile(filepath.Join(addons, marketName, "data", "shared.lua"))
	if strings.Count(string(shared), "{sourceID=") != marketInboxPart || strings.Contains(string(shared), `scanID="s0",`) ||
		!strings.Contains(string(shared), fmt.Sprintf(`scanID="s%d",`, len(points)-1)) || marketVersionLine(shared) == marketVersionLine(first) {
		t.Fatal("запасной data/shared.lua: последние 40 000 наблюдений со своей версией")
	}
	if _, installed, err := marketInstall(addons, cfg, "self", marketHistory{Points: points[:3]}); err != nil || !installed {
		t.Fatal(installed, err)
	}
	if _, err := os.Stat(filepath.Join(shelf, "inbox_2.lua")); !os.IsNotExist(err) {
		t.Fatal("лишняя часть инбокса осталась")
	}
	if pending, installed, err := marketInstall(addons, cfg, "self", marketHistory{Points: points[:3]}); err != nil || pending || installed {
		t.Fatal("повтор без изменений переписал инбокс", pending, installed, err)
	}
}
