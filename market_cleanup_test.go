package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDropMarketFiles(t *testing.T) {
	root := t.TempDir()
	addons := filepath.Join(root, "AddOns")
	home := filepath.Join(root, "home")
	inbox := filepath.Join(addons, marketInboxName)
	put(t, filepath.Join(inbox, marketInboxName+".toc"), "## Interface: 30300\r\n## LoadOnDemand: 1\r\n"+marketInboxMark+"\r\n\r\ninbox.lua\r\n")
	put(t, filepath.Join(inbox, "inbox.lua"), "ManaCodeAuctionScanInbox = {}")
	scan := filepath.Join(addons, marketName, "data", "shared.lua")
	put(t, scan, "local _, ns = ...")
	names := []string{"AuctionMarket.json", "AuctionMarket-status.json", "AuctionMarket-history.json", "AuctionMarket-inbox.lua"}
	for _, n := range names {
		put(t, filepath.Join(home, n), "{}")
	}
	kept := filepath.Join(home, "config.json")
	put(t, kept, "{}")
	gone := dropMarketFiles(addons, home)
	if len(gone) != 1+len(names) {
		t.Fatalf("removed %v", gone)
	}
	if _, err := os.Stat(inbox); !os.IsNotExist(err) {
		t.Fatal("inbox folder left")
	}
	for _, n := range names {
		if _, err := os.Stat(filepath.Join(home, n)); !os.IsNotExist(err) {
			t.Fatalf("%s left", n)
		}
	}
	for _, p := range []string{scan, kept} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s removed: %v", p, err)
		}
	}
	if gone := dropMarketFiles(addons, home); len(gone) != 0 {
		t.Fatalf("second run removed %v", gone)
	}
}

func TestDropMarketKeepsForeignShared(t *testing.T) {
	addons := t.TempDir()
	inbox := filepath.Join(addons, marketInboxName)
	toc := filepath.Join(inbox, marketInboxName+".toc")
	put(t, toc, "## Interface: 30300\n## Title: someone else\n## X-ManaCode-Inbox: 0\n")
	if gone := dropMarketFiles(addons, ""); len(gone) != 0 {
		t.Fatalf("removed %v", gone)
	}
	if _, err := os.Stat(toc); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(toc); err != nil {
		t.Fatal(err)
	}
	if gone := dropMarketFiles(addons, ""); len(gone) != 0 || !isDir(inbox) {
		t.Fatalf("folder without toc removed: %v", gone)
	}
}
