package main

import (
	"testing"
	"time"
)

func TestSelectedAddonIntervalPersistsAndControlsSchedule(t *testing.T) {
	e := newAutoEnv(t)
	start := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	saveConfig(config{Auto: &autoConfig{AddonsAt: start}})
	a := &app{addons: e.addons, cfg: loadConfig()}
	for _, hours := range addonIntervals {
		stale := loadConfig().auto()
		a.selectHours(hours)
		storeAuto(stale)
		cfg := loadConfig().auto()
		if cfg.hours() != hours || a.view().hours != hours {
			t.Fatalf("interval %d was not preserved", hours)
		}
		deadline := start.Add(time.Duration(hours) * time.Hour)
		if cfg.addonsDue(deadline.Add(-time.Second)) || !cfg.addonsDue(deadline) {
			t.Fatalf("wrong check deadline for %d hours", hours)
		}
	}
	a.selectHours(0)
	a.selectHours(-1)
	if loadConfig().auto().hours() != 24 {
		t.Fatal("invalid interval overwrote saved preference")
	}
}
