package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
)

const languageFile = "language.json"

//go:embed locale/en.json
var englishJSON []byte

//go:embed locale/es.json
var spanishJSON []byte

const (
	languageRU int32 = iota
	languageEN
	languageES
)

var activeLanguage atomic.Int32
var english = loadTranslations(englishJSON)
var spanish = loadTranslations(spanishJSON)

type localizedError string

func (e localizedError) Error() string { return tr(string(e)) }

func loadTranslations(data []byte) map[string]string {
	words := map[string]string{}
	if err := json.Unmarshal(data, &words); err != nil {
		panic(err)
	}
	return words
}

func validLanguage(language string) bool {
	return language == "ru" || language == "en" || language == "es"
}

func loadLanguage() string {
	var saved struct {
		Language string `json:"language"`
	}
	b, err := os.ReadFile(homePath(languageFile))
	if err != nil || json.Unmarshal(b, &saved) != nil || !validLanguage(saved.Language) {
		return ""
	}
	return saved.Language
}

func useLanguage(language string) {
	switch language {
	case "en":
		activeLanguage.Store(languageEN)
	case "es":
		activeLanguage.Store(languageES)
	default:
		activeLanguage.Store(languageRU)
	}
}

func saveLanguage(language string) error {
	if !validLanguage(language) {
		return errors.New("Unsupported language / Неподдерживаемый язык")
	}
	p := homePath(languageFile)
	if p == "" {
		return errors.New("Cannot find settings folder / Не найдена папка настроек")
	}
	b, err := json.Marshal(struct {
		Language string `json:"language"`
	}{Language: language})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), "language-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := replaceLanguageFile(tmpPath, p); err != nil {
		return err
	}
	useLanguage(language)
	return nil
}

func tr(source string) string {
	var words map[string]string
	switch activeLanguage.Load() {
	case languageEN:
		words = english
	case languageES:
		words = spanish
	}
	if translated, ok := words[source]; ok {
		return translated
	}
	return source
}
