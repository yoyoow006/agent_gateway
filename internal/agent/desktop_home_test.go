package agent

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
)

func managedPaths(t *testing.T) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, ".agw", "codex-desktop")
	return root, home, filepath.Join(home, "config.toml")
}

func assertDesktopConfig(t *testing.T, path, token, wantBase string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		ModelProvider          string `toml:"model_provider"`
		DisableResponseStorage bool   `toml:"disable_response_storage"`
		ModelProviders         map[string]struct {
			Name    string `toml:"name"`
			BaseURL string `toml:"base_url"`
			WireAPI string `toml:"wire_api"`
			EnvKey  string `toml:"env_key"`
		} `toml:"model_providers"`
	}
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.ModelProvider != "agw" || !cfg.DisableResponseStorage {
		t.Fatalf("cfg = %+v", cfg)
	}
	p := cfg.ModelProviders["agw"]
	if p.Name != "agw" || p.BaseURL != wantBase || p.WireAPI != "responses" || p.EnvKey != "AGW_API_KEY" {
		t.Fatalf("provider = %+v", p)
	}
	if strings.Contains(string(data), token) {
		t.Fatalf("config.toml leaks token %q", token)
	}
	authPath := filepath.Join(filepath.Dir(path), "auth.json")
	authData, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatal(err)
	}
	var auth map[string]string
	if err := json.Unmarshal(authData, &auth); err != nil {
		t.Fatal(err)
	}
	if len(auth) != 1 || auth["OPENAI_API_KEY"] != token {
		t.Fatalf("auth = %+v", auth)
	}
	assertMode(t, authPath, 0o600)
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != want {
		t.Fatalf("%s mode = %o, want %o", path, fi.Mode().Perm(), want)
	}
}

func TestEnsureDesktopHomeCreatesManagedPair(t *testing.T) {
	root, home, cfgPath := managedPaths(t)
	if _, err := EnsureDesktopHome(root, "127.0.0.1:8787", "agw-global"); err != nil {
		t.Fatal(err)
	}
	assertDesktopConfig(t, cfgPath, "agw-global", "http://127.0.0.1:8787/v1")
	assertMode(t, cfgPath, 0o600)
	assertMode(t, home, 0o700)
	entries, err := os.ReadDir(filepath.Join(home, "backups"))
	if err != nil || len(entries) != 2 {
		t.Fatalf("backup entries = %v (%v)", entries, err)
	}
}

func TestEnsureDesktopHomeIPv6URL(t *testing.T) {
	root, _, cfgPath := managedPaths(t)
	if _, err := EnsureDesktopHome(root, "[::1]:8787", "tok"); err != nil {
		t.Fatal(err)
	}
	assertDesktopConfig(t, cfgPath, "tok", "http://[::1]:8787/v1")
}

func TestEnsureDesktopHomeNoOpPreservesMtime(t *testing.T) {
	root, _, cfgPath := managedPaths(t)
	if _, err := EnsureDesktopHome(root, "127.0.0.1:8787", "tok"); err != nil {
		t.Fatal(err)
	}
	authPath := filepath.Join(filepath.Dir(cfgPath), "auth.json")
	cm, _ := os.Stat(cfgPath)
	am, _ := os.Stat(authPath)
	time.Sleep(2 * time.Millisecond)
	if _, err := EnsureDesktopHome(root, "127.0.0.1:8787", "tok"); err != nil {
		t.Fatal(err)
	}
	cm2, _ := os.Stat(cfgPath)
	am2, _ := os.Stat(authPath)
	if !cm.ModTime().Equal(cm2.ModTime()) || !am.ModTime().Equal(am2.ModTime()) {
		t.Fatal("no-op changed mtime")
	}
	entries, _ := os.ReadDir(filepath.Join(filepath.Dir(cfgPath), "backups"))
	if len(entries) != 2 {
		t.Fatalf("no-op created backups: %v", entries)
	}
}

func TestEnsureDesktopHomeChangeCreatesBackupAndTightensPermissions(t *testing.T) {
	root, home, cfgPath := managedPaths(t)
	if _, err := EnsureDesktopHome(root, "127.0.0.1:8787", "old"); err != nil {
		t.Fatal(err)
	}
	oldConfig, _ := os.ReadFile(cfgPath)
	if err := os.Chmod(cfgPath, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureDesktopHome(root, "127.0.0.1:9999", "new"); err != nil {
		t.Fatal(err)
	}
	assertDesktopConfig(t, cfgPath, "new", "http://127.0.0.1:9999/v1")
	backup, err := os.ReadFile(newestBackup(t, filepath.Join(home, "backups"), "config.toml"))
	if err != nil || string(backup) != string(oldConfig) {
		t.Fatalf("backup = %q (%v)", backup, err)
	}
}

func TestEnsureDesktopHomeRejectsUnexpectedEntriesAndInvalidManagedFiles(t *testing.T) {
	root, home, cfgPath := managedPaths(t)
	if _, err := EnsureDesktopHome(root, "127.0.0.1:8787", "tok"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "unexpected"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureDesktopHome(root, "127.0.0.1:8787", "tok"); err == nil || !strings.Contains(err.Error(), "--reset") {
		t.Fatalf("unexpected entry error = %v", err)
	}
	if err := os.Remove(filepath.Join(home, "unexpected")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte("broken ["), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureDesktopHome(root, "127.0.0.1:8787", "tok"); err == nil {
		t.Fatal("invalid TOML should fail")
	}
}

func TestEnsureDesktopHomeRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	agw := filepath.Join(root, ".agw")
	if err := os.MkdirAll(agw, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(agw, "codex-desktop")); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureDesktopHome(root, "127.0.0.1:8787", "tok"); err == nil || !strings.Contains(err.Error(), "符号链接") {
		t.Fatalf("symlink escape error = %v", err)
	}
}

func TestResetDesktopHomeRemovesOnlyManagedDirectory(t *testing.T) {
	root, home, _ := managedPaths(t)
	defaultHome := filepath.Join(t.TempDir(), ".codex")
	if err := os.MkdirAll(filepath.Join(home, "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(defaultHome, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ResetDesktopHome(root); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(home)
	if err != nil || !fi.IsDir() || fi.Mode().Perm() != 0o700 {
		t.Fatalf("home after reset = %+v (%v)", fi, err)
	}
	entries, err := os.ReadDir(home)
	if err != nil || len(entries) != 0 {
		t.Fatalf("reset entries = %v (%v)", entries, err)
	}
	if _, err := os.Stat(filepath.Join(defaultHome, "config.toml")); err == nil {
		t.Fatal("default home should remain untouched")
	}
}

func TestResetDesktopHomeRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "sentinel"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".agw"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, ".agw", "codex-desktop")); err != nil {
		t.Fatal(err)
	}
	if err := ResetDesktopHome(root); err == nil || !strings.Contains(err.Error(), "符号链接") {
		t.Fatalf("reset symlink error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "sentinel")); err != nil {
		t.Fatal("outside unexpectedly removed")
	}
}

func TestEnsureDesktopHomeRollsBackPairWhenSecondWriteFails(t *testing.T) {
	root, home, cfgPath := managedPaths(t)
	if _, err := EnsureDesktopHome(root, "127.0.0.1:8787", "old"); err != nil {
		t.Fatal(err)
	}
	cfgBefore, _ := os.ReadFile(cfgPath)
	authPath := filepath.Join(home, "auth.json")
	authBefore, _ := os.ReadFile(authPath)
	_, _ = EnsureDesktopHome(root, "127.0.0.1:8787", "old") // normalize backup state

	var fail func(path string) error
	fail = func(path string) error {
		if strings.HasSuffix(path, ".tmp") && strings.HasPrefix(filepath.Base(path), "auth.json.") {
			return errors.New("boom")
		}
		return nil
	}
	_, err := EnsureDesktopHomeWithFailer(root, "127.0.0.1:9999", "new", fail)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("write failure = %v", err)
	}
	cfgAfter, _ := os.ReadFile(cfgPath)
	authAfter, _ := os.ReadFile(authPath)
	if string(cfgAfter) != string(cfgBefore) || string(authAfter) != string(authBefore) {
		t.Fatalf("rollback failed: cfg=%q auth=%q", cfgAfter, authAfter)
	}
}

func newestBackup(t *testing.T, dir, suffix string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var found string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasSuffix(name, suffix) && !strings.HasSuffix(name, ".tmp") && name > found {
			found = name
		}
	}
	if found == "" {
		t.Fatalf("missing %s backup in %s", suffix, dir)
	}
	return filepath.Join(dir, found)
}
func TestDesktopRollbackRemovesTemporaryFiles(t *testing.T) {
	root, home, cfgPath := managedPaths(t)
	if _, err := EnsureDesktopHome(root, "127.0.0.1:8787", "old"); err != nil {
		t.Fatal(err)
	}
	authPath := filepath.Join(home, "auth.json")
	failer := func(path string) error {
		if strings.HasSuffix(path, ".tmp") && strings.HasPrefix(filepath.Base(path), "auth.json.") {
			return os.ErrPermission
		}
		return nil
	}
	if _, err := EnsureDesktopHomeWithFailer(root, "127.0.0.1:9999", "new", failer); err == nil {
		t.Fatal("expected second write failure")
	}
	for _, path := range []string{cfgPath + ".tmp", authPath + ".tmp"} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("temporary rollback file remains: %s (%v)", path, err)
		}
	}
}
