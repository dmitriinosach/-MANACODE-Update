package main

import "strings"

func luaLongOpen(s string, i int) int {
	if i >= len(s) || s[i] != '[' {
		return -1
	}
	j := i + 1
	for j < len(s) && s[j] == '=' {
		j++
	}
	if j < len(s) && s[j] == '[' {
		return j - i - 1
	}
	return -1
}

func luaLongEnd(s string, i, level int) int {
	close := "]" + strings.Repeat("=", level) + "]"
	k := strings.Index(s[i:], close)
	if k < 0 {
		return len(s)
	}
	return i + k + len(close)
}

func luaStrip(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	line := true
	pendingSpace := false
	emit := func(t string) {
		if pendingSpace && b.Len() > 0 && !line {
			b.WriteByte(' ')
		}
		pendingSpace = false
		b.WriteString(t)
		line = false
	}
	newline := func() {
		pendingSpace = false
		b.WriteByte('\n')
		line = true
	}
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '\r':
			i++
		case c == '\n':
			newline()
			i++
		case c == ' ' || c == '\t' || c == '\f' || c == '\v':
			pendingSpace = true
			i++
		case c == '-' && i+1 < len(s) && s[i+1] == '-':
			if lvl := luaLongOpen(s, i+2); lvl >= 0 {
				end := luaLongEnd(s, i+2+lvl+2, lvl)
				for n := strings.Count(s[i:end], "\n"); n > 0; n-- {
					newline()
				}
				pendingSpace = true
				i = end
				continue
			}
			for i < len(s) && s[i] != '\n' {
				i++
			}
		case c == '[' && luaLongOpen(s, i) >= 0:
			lvl := luaLongOpen(s, i)
			end := luaLongEnd(s, i+lvl+2, lvl)
			emit(s[i:end])
			i = end
		case c == '"' || c == '\'':
			j := i + 1
			for j < len(s) && s[j] != c {
				if s[j] == '\\' {
					j++
				} else if s[j] == '\n' {
					break
				}
				j++
			}
			j = min(j+1, len(s))
			emit(s[i:j])
			i = j
		default:
			j := i + 1
			for j < len(s) && !strings.ContainsRune(" \t\r\n\f\v-[\"'", rune(s[j])) {
				j++
			}
			if j == i+1 && c == '-' {
				emit("-")
				i++
				continue
			}
			emit(s[i:j])
			i = j
		}
	}
	return b.String()
}
