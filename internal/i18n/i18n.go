// Package i18n provides embedded en/zh message catalogs and lookup with
// Sprintf-style argument interpolation.
package i18n

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

//go:embed en.json
var enData []byte

//go:embed zh.json
var zhData []byte

var (
	mu     sync.RWMutex
	lang   = "en"
	enDict map[string]string
	zhDict map[string]string
	once   sync.Once
)

func loadDictionaries() {
	once.Do(func() {
		enDict = loadJSON(enData)
		zhDict = loadJSON(zhData)
	})
}

func loadJSON(data []byte) map[string]string {
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return map[string]string{}
	}
	return m
}

// SetLanguage sets the global display language. Only "en" and "zh" are accepted.
func SetLanguage(l string) {
	mu.Lock()
	defer mu.Unlock()
	if l == "zh" || l == "en" {
		lang = l
	}
}

// GetLanguage returns the current display language.
func GetLanguage() string {
	mu.RLock()
	defer mu.RUnlock()
	return lang
}

// T looks up a translation key in the current language dictionary.
// Falls back to English if the key is missing in the target language.
// Supports Sprintf-style positional args.
func T(key string, args ...any) string {
	loadDictionaries()
	mu.RLock()
	currentLang := lang
	mu.RUnlock()

	var val string
	var ok bool
	if currentLang == "zh" {
		val, ok = zhDict[key]
	}
	if !ok {
		val, ok = enDict[key]
	}
	if !ok {
		return key
	}
	if len(args) > 0 {
		// Dictionary strings are written like fmt.Errorf templates and may
		// use %w for wrapped errors, but Sprintf does not understand that
		// verb — translate it so errors don't render as "%!w(...)".
		val = strings.ReplaceAll(val, "%w", "%v")
		return fmt.Sprintf(val, args...)
	}
	return val
}

// DetectLanguage resolves the language from the priority chain:
//
//	flag > env > config > systemLang > default "en"
func DetectLanguage(flagVal, envVal, configVal, systemLang string) string {
	if flagVal != "" {
		return normalizeLang(flagVal)
	}
	if envVal != "" {
		return normalizeLang(envVal)
	}
	if configVal != "" {
		return normalizeLang(configVal)
	}
	if strings.HasPrefix(systemLang, "zh") {
		return "zh"
	}
	return "en"
}

func normalizeLang(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if strings.HasPrefix(s, "zh") {
		return "zh"
	}
	return "en"
}
