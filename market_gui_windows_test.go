package main

import "testing"

func TestMarketFieldFocusDoesNotBlockClose(t *testing.T) {
	u := &gui{app: &app{}, marketFocus: "marketloginfield"}
	if u.busyNow() {
		t.Fatal("курсор в поле входа не даёт закрыть окно")
	}
	u.app.marketBusy = true
	if !u.busyNow() {
		t.Fatal("идущий обмен должен держать окно")
	}
}
