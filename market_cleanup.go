package main

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
)

const (
	marketInboxName = "ManaCode_AuctionScan_Shared"
	marketInboxMark = "## X-ManaCode-Inbox: 1"
)

func dropMarket(addons string) {
	for _, p := range dropMarketFiles(addons, appHome()) {
		autoLog("обмен рынка убран: удалено %s", p)
	}
}

func dropMarketFiles(addons, home string) []string {
	var gone []string
	if dir := folderIn(addons, marketInboxName); dir != "" && marketInbox(dir) {
		if err := os.RemoveAll(dir); err == nil {
			gone = append(gone, dir)
		}
	}
	if home == "" {
		return gone
	}
	found, _ := filepath.Glob(filepath.Join(globQuote(home), "AuctionMarket*"))
	for _, p := range found {
		if st, err := os.Lstat(p); err != nil || !st.Mode().IsRegular() {
			continue
		}
		if err := os.Remove(p); err == nil {
			gone = append(gone, p)
		}
	}
	return gone
}

func marketInbox(dir string) bool {
	st, err := os.Lstat(dir)
	if err != nil || !st.IsDir() || st.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return false
	}
	b, err := os.ReadFile(filepath.Join(dir, marketInboxName+".toc"))
	if err != nil {
		return false
	}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) == marketInboxMark {
			return true
		}
	}
	return false
}
