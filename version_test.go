package main

import "testing"

func TestSemverOrder(t *testing.T) {
	order := []string{
		"0.1.0",
		"0.2.0-alpha",
		"0.2.0-beta",
		"0.2.0-beta.1",
		"0.2.0-beta.2",
		"v0.2.0-beta.10",
		"0.2.0-rc.1",
		"0.2.0",
		"0.2.1-beta.1",
		"v0.2.1",
		"0.10.0",
		"1.0.0",
	}
	for i := range order {
		for j := range order {
			got := compareVersions(order[i], order[j])
			want := cmpInt(i, j)
			if got != want {
				t.Errorf("compare(%q, %q) = %d, want %d", order[i], order[j], got, want)
			}
		}
	}
}

func TestSemverBoberTags(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v0.16.0", "0.15.9", true},
		{"v0.16.0", "0.16.0", false},
		{"0.16.0", "v0.16.0", false},
		{"v0.16.1", "0.16.0", true},
		{"0.15.0", "0.16.0", false},
		{"0.2.0", "0.2.0-beta.3", true},
		{"0.2.0-beta.3", "0.2.0", false},
		{"0.2", "0.2.0", false},
		{"0.2.1", "0.2", true},
		{"0.2.0+build.5", "0.2.0", false},
		{"b10", "b9", true},
		{"0.12.0", "b11", true},
		{"b11", "0.10.0", true},
		{"b11", "0.11.0", false},
	}
	for _, c := range cases {
		if got := newer(c.a, c.b); got != c.want {
			t.Errorf("newer(%q, %q) = %t, want %t", c.a, c.b, got, c.want)
		}
	}
}

func TestSemverParse(t *testing.T) {
	bad := []string{"", "b11", "1.2.3.4", "1..2", "1.2.3-", "1.2.3-beta..1", "x.1.0"}
	for _, s := range bad {
		if _, ok := parseSemver(s); ok {
			t.Errorf("parseSemver(%q) должен не разобраться", s)
		}
	}
	if !isPrerelease("v0.2.0-beta.1") || isPrerelease("v0.2.0") || isPrerelease("b11") {
		t.Error("isPrerelease ошибается")
	}
}
