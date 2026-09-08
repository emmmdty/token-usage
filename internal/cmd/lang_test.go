package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/emmmdty/token-usage/internal/config"
	"github.com/emmmdty/token-usage/internal/i18n"
)

func TestLangCmd_NoArgs(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	i18n.SetLanguage("en")

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	_, _ = config.LoadOrCreateConfig(configPath)

	origGetConfigPath := getConfigPath
	getConfigPath = func() (string, error) { return configPath, nil }
	defer func() {
		getConfigPath = origGetConfigPath
		i18n.SetLanguage("en")
	}()

	rootCmd.SetArgs([]string{"lang"})
	buf := captureOutput(t, func() {
		_ = rootCmd.Execute()
	})
	if !strings.Contains(buf, "Current language: en") {
		t.Errorf("expected 'Current language: en', got: %s", buf)
	}
}

func TestLangCmd_SetZh(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	i18n.SetLanguage("en")

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	_, _ = config.LoadOrCreateConfig(configPath)

	origGetConfigPath := getConfigPath
	getConfigPath = func() (string, error) { return configPath, nil }
	defer func() {
		getConfigPath = origGetConfigPath
		i18n.SetLanguage("en")
	}()

	rootCmd.SetArgs([]string{"lang", "zh"})
	buf := captureOutput(t, func() {
		_ = rootCmd.Execute()
	})

	cfg, err := config.LoadOrCreateConfig(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	if cfg.Language != "zh" {
		t.Errorf("expected config language 'zh', got %q", cfg.Language)
	}
	if !strings.Contains(buf, "中文") {
		t.Errorf("expected Chinese confirmation, got: %s", buf)
	}
}

func TestLangCmd_SetEn(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	i18n.SetLanguage("en")

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	cfg, _ := config.LoadOrCreateConfig(configPath)
	cfg.Language = "zh"
	_ = config.SaveConfig(cfg, configPath)

	origGetConfigPath := getConfigPath
	getConfigPath = func() (string, error) { return configPath, nil }
	defer func() {
		getConfigPath = origGetConfigPath
		i18n.SetLanguage("en")
	}()

	rootCmd.SetArgs([]string{"lang", "en"})
	buf := captureOutput(t, func() {
		_ = rootCmd.Execute()
	})

	cfg, err := config.LoadOrCreateConfig(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	if cfg.Language != "en" {
		t.Errorf("expected config language 'en', got %q", cfg.Language)
	}
	if !strings.Contains(buf, "Language switched to English") {
		t.Errorf("expected English confirmation, got: %s", buf)
	}
}

func TestLangCmd_InvalidLang(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	i18n.SetLanguage("en")

	rootCmd.SetArgs([]string{"lang", "fr"})
	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error for invalid language")
	}
	if !strings.Contains(err.Error(), "unsupported language") && !strings.Contains(err.Error(), "不支持的语言") {
		t.Errorf("expected 'unsupported language' error, got: %v", err)
	}
}

func TestLangCmd_Alias(t *testing.T) {
	t.Setenv("LANG", "en_US.UTF-8")
	i18n.SetLanguage("en")

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	_, _ = config.LoadOrCreateConfig(configPath)

	origGetConfigPath := getConfigPath
	getConfigPath = func() (string, error) { return configPath, nil }
	defer func() {
		getConfigPath = origGetConfigPath
		i18n.SetLanguage("en")
	}()

	rootCmd.SetArgs([]string{"language"})
	buf := captureOutput(t, func() {
		_ = rootCmd.Execute()
	})
	if !strings.Contains(buf, "Current language: en") {
		t.Errorf("expected 'Current language: en' via alias, got: %s", buf)
	}
}

func captureOutput(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	fn()

	_ = w.Close()
	os.Stdout = old

	var buf strings.Builder
	b := make([]byte, 4096)
	for {
		n, err := r.Read(b)
		if n > 0 {
			buf.Write(b[:n])
		}
		if err != nil {
			break
		}
	}
	return buf.String()
}
