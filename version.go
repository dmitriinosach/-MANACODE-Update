package main

import (
	"regexp"
	"strconv"
	"strings"
)

type semver struct {
	core [3]int
	pre  []string
}

func parseSemver(s string) (semver, bool) {
	var v semver
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(strings.TrimPrefix(s, "v"), "V")
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	core, pre, hasPre := strings.Cut(s, "-")
	parts := strings.Split(core, ".")
	if core == "" || len(parts) > 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || p == "" || p[0] == '+' {
			return v, false
		}
		v.core[i] = n
	}
	if hasPre {
		if pre == "" {
			return v, false
		}
		v.pre = strings.Split(pre, ".")
		for _, id := range v.pre {
			if id == "" {
				return v, false
			}
		}
	}
	return v, true
}

func isNumeric(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

func comparePre(a, b string) int {
	an, bn := isNumeric(a), isNumeric(b)
	switch {
	case an && bn:
		x, _ := strconv.Atoi(a)
		y, _ := strconv.Atoi(b)
		return cmpInt(x, y)
	case an:
		return -1
	case bn:
		return 1
	}
	return strings.Compare(a, b)
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func (a semver) compare(b semver) int {
	for i := range a.core {
		if c := cmpInt(a.core[i], b.core[i]); c != 0 {
			return c
		}
	}
	switch {
	case len(a.pre) == 0 && len(b.pre) == 0:
		return 0
	case len(a.pre) == 0:
		return 1
	case len(b.pre) == 0:
		return -1
	}
	for i := 0; i < len(a.pre) && i < len(b.pre); i++ {
		if c := comparePre(a.pre[i], b.pre[i]); c != 0 {
			return c
		}
	}
	return cmpInt(len(a.pre), len(b.pre))
}

var reDigits = regexp.MustCompile(`\d+`)

func verNum(s string) []int {
	var out []int
	if strings.HasPrefix(s, "b") {
		out = append(out, 0)
	}
	for _, part := range reDigits.FindAllString(s, -1) {
		v, _ := strconv.Atoi(part)
		out = append(out, v)
	}
	return out
}

func compareDigits(a, b string) int {
	x, y := verNum(a), verNum(b)
	for i := 0; i < len(x) || i < len(y); i++ {
		var p, q int
		if i < len(x) {
			p = x[i]
		}
		if i < len(y) {
			q = y[i]
		}
		if c := cmpInt(p, q); c != 0 {
			return c
		}
	}
	return 0
}

func compareVersions(a, b string) int {
	x, okx := parseSemver(a)
	y, oky := parseSemver(b)
	if okx && oky {
		return x.compare(y)
	}
	return compareDigits(a, b)
}

func newer(a, b string) bool { return compareVersions(a, b) > 0 }

func isPrerelease(s string) bool {
	v, ok := parseSemver(s)
	return ok && len(v.pre) > 0
}
