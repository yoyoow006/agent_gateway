package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseEnvFile(t *testing.T) {
	got, err := ParseEnvFile([]byte(`
# 注释行
PLAIN=sk-plain

export EXPORTED=sk-exported
QUOTED="sk with spaces"
SINGLE='sk-single-quoted'
SPACED = sk-trimmed
EMPTY=
`))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"PLAIN":    "sk-plain",
		"EXPORTED": "sk-exported",
		"QUOTED":   "sk with spaces",
		"SINGLE":   "sk-single-quoted",
		"SPACED":   "sk-trimmed",
		"EMPTY":    "",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("多余条目: %+v", got)
	}
}

func TestParseEnvFileSyntaxError(t *testing.T) {
	_, err := ParseEnvFile([]byte("GOOD=1\nthis-is-bad\nALSO=2"))
	if err == nil || !strings.Contains(err.Error(), "第 2 行") {
		t.Fatalf("应带行号报错: %v", err)
	}
	_, err = ParseEnvFile([]byte("=novalue"))
	if err == nil || !strings.Contains(err.Error(), "变量名") {
		t.Fatalf("空变量名应报错: %v", err)
	}
}

func TestLoadEnvFileMissingIsNil(t *testing.T) {
	if warn, err := LoadEnvFile(filepath.Join(t.TempDir(), ".env")); err != nil || warn != "" {
		t.Fatalf("缺失文件应 (nil,\"\",nil): %q %v", warn, err)
	}
}

func TestLoadEnvFileNoOverrideAndPermWarning(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	os.WriteFile(path, []byte("AGW_DOTENV_A=from-file\nAGW_DOTENV_B=from-file\n"), 0o644)

	t.Setenv("AGW_DOTENV_A", "from-real-env")
	warn, err := LoadEnvFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if warn == "" || !strings.Contains(warn, "0600") {
		t.Errorf("0644 权限应产生警告: %q", warn)
	}
	if os.Getenv("AGW_DOTENV_A") != "from-real-env" {
		t.Error("真实环境变量不得被 .env 覆盖")
	}
	if os.Getenv("AGW_DOTENV_B") != "from-file" {
		t.Error("缺失变量应从 .env 注入")
	}
}

func TestLoadEnvFileTightPermsNoWarning(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	os.WriteFile(path, []byte("AGW_DOTENV_C=x\n"), 0o600)
	if warn, err := LoadEnvFile(path); err != nil || warn != "" {
		t.Fatalf("0600 不应警告: %q %v", warn, err)
	}
}
