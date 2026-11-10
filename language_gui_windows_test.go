package main

import "testing"

func TestLanguageLinkDuringBackgroundWork(t *testing.T) {
	for _, tc := range []struct {
		name     string
		state    *app
		focus    string
		wantOpen bool
	}{
		{name: "idle", state: &app{}, wantOpen: true},
		{name: "market sync", state: &app{marketBusy: true}, wantOpen: true},
		{name: "market check", state: &app{marketChecking: true}, wantOpen: true},
		{name: "editing market login", state: &app{}, focus: "marketloginfield", wantOpen: true},
		{name: "addon install", state: &app{busy: "rh"}, wantOpen: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := &gui{app: tc.state, marketFocus: tc.focus}
			u.addLanguageLink(box{x: 168, y: 65, w: 62, h: 17})
			link, found := u.hitAt(190, 72)
			if !found || link.id != "language" || link.on != tc.wantOpen {
				t.Fatalf("language link: found=%v, enabled=%v; want enabled=%v", found, link.on, tc.wantOpen)
			}
		})
	}
}
