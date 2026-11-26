package main

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

var formatVerbPattern = regexp.MustCompile(`%(?:%|(?:\[[0-9]+\])?[-+# 0]*[0-9]*(?:\.[0-9]+)?[bcdefgoOpqstTUVvwxX])`)

func formatVerbs(s string) []string {
	parts := formatVerbPattern.FindAllString(s, -1)
	verbs := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "%%" {
			verbs = append(verbs, part[len(part)-1:])
		}
	}
	return verbs
}

func TestTranslationsHaveMatchingFormatVerbs(t *testing.T) {
	for language, words := range map[string]map[string]string{"en": english, "es": spanish} {
		for source, translated := range words {
			sourceVerbs, translatedVerbs := formatVerbs(source), formatVerbs(translated)
			if strings.Join(sourceVerbs, ",") != strings.Join(translatedVerbs, ",") {
				t.Errorf("translation format verbs differ for %q in %s: source %v, translation %v", source, language, sourceVerbs, translatedVerbs)
			}
		}
	}
}

func TestTranslationsContainNoCyrillic(t *testing.T) {
	for language, words := range map[string]map[string]string{"en": english, "es": spanish} {
		for source, translated := range words {
			for _, r := range translated {
				if unicode.In(r, unicode.Cyrillic) {
					t.Errorf("translation for %q in %s contains Cyrillic: %q", source, language, translated)
					break
				}
			}
		}
	}
}

func TestTranslationDictionariesHaveSameKeys(t *testing.T) {
	for source := range english {
		if translated, ok := spanish[source]; !ok || translated == "" {
			t.Errorf("missing Spanish translation for %q", source)
		}
	}
	for source := range spanish {
		if _, ok := english[source]; !ok {
			t.Errorf("Spanish dictionary has unknown key %q", source)
		}
	}
}

func TestLiteralTranslationsArePresent(t *testing.T) {
	seen := map[string]bool{}
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 {
				return true
			}
			fn, ok := call.Fun.(*ast.Ident)
			if !ok || fn.Name != "tr" {
				return true
			}
			literal, ok := call.Args[0].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			source, err := strconv.Unquote(literal.Value)
			if err == nil {
				seen[source] = true
			}
			return true
		})
	}
	for source := range seen {
		if _, ok := english[source]; !ok {
			t.Errorf("missing English translation for tr(%q)", source)
		}
	}
}

func TestLanguagePreferenceCanBeReplaced(t *testing.T) {
	oldConfigDir := userConfigDir
	oldLanguage := activeLanguage.Load()
	configDir := t.TempDir()
	userConfigDir = func() (string, error) { return configDir, nil }
	t.Cleanup(func() {
		userConfigDir = oldConfigDir
		activeLanguage.Store(oldLanguage)
	})
	for _, language := range []string{"ru", "en", "es", "ru"} {
		if err := saveLanguage(language); err != nil {
			t.Fatal(err)
		}
		if got := loadLanguage(); got != language {
			t.Fatalf("loadLanguage() after saving %s = %q", language, got)
		}
		useLanguage(loadLanguage())
		want := "каждый час"
		switch language {
		case "en":
			want = english[want]
		case "es":
			want = spanish[want]
		}
		if got := hoursRU(1); got != want {
			t.Errorf("saved language %s: hoursRU(1) = %q, want %q", language, got, want)
		}
	}
}

func TestLanguageFallbackPreservesUnknownText(t *testing.T) {
	oldLanguage := activeLanguage.Load()
	t.Cleanup(func() { activeLanguage.Store(oldLanguage) })
	for _, language := range []string{"ru", "en", "es"} {
		useLanguage(language)
		for _, source := range []string{"Игрок-Гордунни", `D:\Игры\WoW`, "CustomAddon"} {
			if got := tr(source); got != source {
				t.Errorf("tr(%q) in %s = %q", source, language, got)
			}
		}
	}
	useLanguage("unsupported")
	if got := tr("каждый час"); got != "каждый час" {
		t.Errorf("unknown language fallback = %q", got)
	}
}

func TestSpanishPluralAndDate(t *testing.T) {
	oldLanguage := activeLanguage.Load()
	t.Cleanup(func() { activeLanguage.Store(oldLanguage) })
	useLanguage("es")
	for _, n := range []int{0, 1, 2, 5, 21, 101} {
		want := "muchos"
		if n == 1 {
			want = "uno"
		}
		if got := localizedPlural(n, "uno", "pocos", "muchos"); got != want {
			t.Errorf("localizedPlural(%d) = %q, want %q", n, got, want)
		}
	}
	if got := bakedRU("2026-10-29T12:04:00Z"); got != "29/10 15:04 MSK" {
		t.Errorf("Spanish date = %q", got)
	}
}

func TestCustomErrorsFollowSelectedLanguage(t *testing.T) {
	oldLanguage := activeLanguage.Load()
	t.Cleanup(func() { activeLanguage.Store(oldLanguage) })
	archive := customTestZip(t, map[string]string{"README.md": "no addon toc"})
	for _, tc := range []struct {
		language, link, archive string
	}{
		{"en", "Only HTTPS links to github.com are supported", "No WoW addon with a .toc file was found"},
		{"es", "solo se admiten enlaces HTTPS a github.com", "no se encontró ningún addon de WoW con archivo .toc"},
		{"ru", "поддерживаются только HTTPS-ссылки", "в архиве не найден аддон WoW с .toc"},
	} {
		useLanguage(tc.language)
		if _, err := githubRepo(""); err == nil || !strings.Contains(err.Error(), tc.link) {
			t.Errorf("githubRepo() error in %s = %v, want it to contain %q", tc.language, err, tc.link)
		}
		if _, err := unpackTest(t, archive); err == nil || !strings.Contains(err.Error(), tc.archive) {
			t.Errorf("unpackCustom() error in %s = %v, want it to contain %q", tc.language, err, tc.archive)
		}
	}
}

func TestWrappedSentinelErrorFollowsSelectedLanguage(t *testing.T) {
	oldLanguage := activeLanguage.Load()
	t.Cleanup(func() { activeLanguage.Store(oldLanguage) })
	for _, tc := range []struct {
		language, want string
	}{
		{"en", "Another updater is installing an addon"},
		{"es", "otro actualizador está instalando un addon"},
		{"ru", "другая обновлялка сейчас ставит аддон"},
	} {
		useLanguage(tc.language)
		wrapped := fmt.Errorf("operation failed: %w", errBusy)
		if !errors.Is(wrapped, errBusy) {
			t.Errorf("errors.Is(wrapped, errBusy) is false in %s", tc.language)
		}
		if !strings.Contains(wrapped.Error(), tc.want) {
			t.Errorf("wrapped error in %s = %q, want it to contain %q", tc.language, wrapped.Error(), tc.want)
		}
	}
}
