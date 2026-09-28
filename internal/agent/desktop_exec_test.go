package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func desktopRepo(t *testing.T) string {
	t.Helper()
	return writeRepo(t, map[string]string{
		"config/local.toml": `
[gateway]
default_token = "agw-global"

[projects.foo]
token = "agw-foo"
`,
		"projects/foo/.keep": "",
	})
}

func TestPrepareExecCodexDesktopUsesProjectAndIsolatedHome(t *testing.T) {
	root := desktopRepo(t)
	defaultHome := t.TempDir()
	defaultConfig := filepath.Join(defaultHome, "config.toml")
	defaultAuth := filepath.Join(defaultHome, "auth.json")
	if err := os.WriteFile(defaultConfig, []byte("model = \"user\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(defaultAuth, []byte(`{"tokens":{"oauth":"keep"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", defaultHome)

	app := filepath.Join(t.TempDir(), "ChatGPT")
	writeDesktopExecutable(t, app)
	t.Setenv("AGW_CODEX_APP", app)

	env, dir, argv, err := PrepareExec(root, KindCodexDesktop, "foo", []string{"--flag"})
	if err != nil {
		t.Fatal(err)
	}
	managed := DesktopHome(root)
	if env["CODEX_HOME"] != managed {
		t.Fatalf("CODEX_HOME = %q, want %q", env["CODEX_HOME"], managed)
	}
	if _, ok := env["AGW_API_KEY"]; ok {
		t.Fatal("desktop token must not enter process env")
	}
	if dir != filepath.Join(root, "projects", "foo") {
		t.Fatalf("dir = %q", dir)
	}
	if len(argv) != 2 || argv[0] != app || argv[1] != "--flag" {
		t.Fatalf("argv = %v", argv)
	}
	assertDesktopConfig(t, filepath.Join(managed, "config.toml"), "agw-foo", "http://127.0.0.1:8787/v1")
	if got, _ := os.ReadFile(defaultConfig); string(got) != "model = \"user\"\n" {
		t.Fatalf("default config changed: %q", got)
	}
	if got, _ := os.ReadFile(defaultAuth); string(got) != `{"tokens":{"oauth":"keep"}}` {
		t.Fatalf("default auth changed: %q", got)
	}
}

func TestPrepareExecCodexDesktopGlobalToken(t *testing.T) {
	root := desktopRepo(t)
	app := filepath.Join(t.TempDir(), "Codex")
	writeDesktopExecutable(t, app)
	env, _, _, err := PrepareExec(root, KindCodexDesktop, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if env["CODEX_HOME"] != DesktopHome(root) {
		t.Fatalf("env = %+v", env)
	}
	assertDesktopConfig(t, filepath.Join(DesktopHome(root), "config.toml"), "agw-global", "http://127.0.0.1:8787/v1")
}

func TestPrepareExecCodexDesktopMissingAppDoesNotWriteHome(t *testing.T) {
	root := desktopRepo(t)
	t.Setenv("AGW_CODEX_APP", filepath.Join(t.TempDir(), "missing", "ChatGPT"))
	if _, _, _, err := PrepareExec(root, KindCodexDesktop, "", nil); err == nil {
		t.Fatal("missing app should fail")
	}
	if _, err := os.Stat(DesktopHome(root)); !os.IsNotExist(err) {
		t.Fatalf("managed home unexpectedly exists: %v", err)
	}
}

func TestPrepareExecCodexDesktopAutomaticDiscoveryOnlyLinux(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("production resolver correctly uses host Linux; non-Linux fail-closed is covered by resolver test")
	}
	t.Setenv("AGW_CODEX_APP", "")
	_, _, _, err := PrepareExec(desktopRepo(t), KindCodexDesktop, "", nil)
	if err == nil || !strings.Contains(err.Error(), "仅支持 Linux") {
		t.Fatalf("unsupported error = %v", err)
	}
}

func TestPrepareResetDesktopHomeDoesNotResolveAppOrWriteToken(t *testing.T) {
	root := desktopRepo(t)
	home := DesktopHome(root)
	if err := os.MkdirAll(filepath.Join(home, "stale"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGW_CODEX_APP", "/definitely/missing/Codex")
	if err := PrepareResetDesktop(root); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(home)
	if err != nil || len(entries) != 0 {
		t.Fatalf("reset entries = %v (%v)", entries, err)
	}
	if _, err := os.Stat(filepath.Join(home, "auth.json")); !os.IsNotExist(err) {
		t.Fatalf("reset should not write auth: %v", err)
	}
}
