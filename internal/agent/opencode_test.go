package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type opencodeConfig struct {
	Provider struct {
		Agw struct {
			NPM     string `json:"npm"`
			Name    string `json:"name"`
			Options struct {
				BaseURL string `json:"baseURL"`
				APIKey  string `json:"apiKey"`
			} `json:"options"`
			Models map[string]struct {
				Name string `json:"name"`
			} `json:"models"`
		} `json:"agw"`
	} `json:"provider"`
}

func TestOpenCodeConfigContent(t *testing.T) {
	content, err := OpenCodeConfigContent("127.0.0.1:8787")
	if err != nil {
		t.Fatal(err)
	}
	var cfg opencodeConfig
	if err := json.Unmarshal([]byte(content), &cfg); err != nil {
		t.Fatalf("OPENCODE_CONFIG_CONTENT is not valid JSON (%q): %v", content, err)
	}
	agw := cfg.Provider.Agw
	if agw.NPM != "@ai-sdk/openai-compatible" || agw.Name != "agw" {
		t.Fatalf("provider identity = %+v", agw)
	}
	if agw.Options.BaseURL != "http://127.0.0.1:8787/v1" {
		t.Fatalf("baseURL = %q", agw.Options.BaseURL)
	}
	if agw.Options.APIKey != "{env:AGW_API_KEY}" {
		t.Fatalf("apiKey = %q", agw.Options.APIKey)
	}
	if len(agw.Models) != 1 || agw.Models["agw"].Name != "agw" {
		t.Fatalf("models = %+v", agw.Models)
	}
	if strings.Contains(content, "agw-") || strings.Contains(content, "sk-") {
		t.Fatalf("inline config leaks token-like value: %q", content)
	}
}

func TestOpenCodeConfigContentIPv6(t *testing.T) {
	content, err := OpenCodeConfigContent("[::1]:8787")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, `"baseURL":"http://[::1]:8787/v1"`) {
		t.Fatalf("content = %q", content)
	}
}

func TestPrepareExecOpenCodeProjectProfile(t *testing.T) {
	root := writeRepo(t, map[string]string{
		"config/local.toml": `
[gateway]
default_token = "agw-global"

[projects.foo]
token = "agw-foo"
`,
		"projects/foo/.keep": "",
	})
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	userConfigDir := filepath.Join(home, ".config", "opencode")
	os.MkdirAll(userConfigDir, 0o755)
	userConfig := filepath.Join(userConfigDir, "opencode.json")
	os.WriteFile(userConfig, []byte(`{"theme":"user"}`), 0o600)
	projectConfig := filepath.Join(root, "projects", "foo", "opencode.json")
	os.WriteFile(projectConfig, []byte(`{"plugin":["user-plugin"]}`), 0o600)

	globalSnapshot, globalMtime, _ := snapshotFile(t, userConfig)
	projectSnapshot, projectMtime, _ := snapshotFile(t, projectConfig)
	beforeEntries, _ := os.ReadDir(userConfigDir)

	env, dir, argv, err := PrepareExec(root, KindOpenCode, "foo", []string{"--model", "agw/custom", "--standalone"})
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join(root, "projects", "foo") {
		t.Fatalf("dir = %q", dir)
	}
	if len(env) != 2 || env["AGW_API_KEY"] != "agw-foo" || env["OPENCODE_CONFIG_CONTENT"] == "" {
		t.Fatalf("env = %v", env)
	}
	if strings.Contains(env["OPENCODE_CONFIG_CONTENT"], "agw-foo") {
		t.Fatalf("inline config contains selected token: %q", env["OPENCODE_CONFIG_CONTENT"])
	}
	wantArgv := []string{"opencode", "--model", "agw/custom", "--standalone"}
	if !reflect.DeepEqual(argv, wantArgv) {
		t.Fatalf("argv = %v, want %v", argv, wantArgv)
	}

	globalNow, globalNowMtime, _ := snapshotFile(t, userConfig)
	projectNow, projectNowMtime, _ := snapshotFile(t, projectConfig)
	if globalNow != globalSnapshot || !globalNowMtime.Equal(globalMtime) {
		t.Error("user global OpenCode config changed")
	}
	if projectNow != projectSnapshot || !projectNowMtime.Equal(projectMtime) {
		t.Error("project OpenCode config changed")
	}
	afterEntries, _ := os.ReadDir(userConfigDir)
	if !reflect.DeepEqual(entryNames(beforeEntries), entryNames(afterEntries)) {
		t.Fatalf("user config dir changed: %v -> %v", entryNames(beforeEntries), entryNames(afterEntries))
	}
}

func TestInstallOpenCodeMajorConstraintAndZeroTouch(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	configDir := filepath.Join(home, ".config", "opencode")
	os.MkdirAll(configDir, 0o755)
	userConfig := filepath.Join(configDir, "opencode.json")
	os.WriteFile(userConfig, []byte(`{"theme":"user"}`), 0o600)
	before, beforeMtime, _ := snapshotFile(t, userConfig)
	beforeEntries, _ := os.ReadDir(configDir)

	runner := &okRunner{}
	if err := InstallOpenCode(Options{Home: home, Root: root, Listen: "127.0.0.1:8787", Runner: runner}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"npm --version",
		"npm install -g @opencode/cli@^2",
	}
	if !reflect.DeepEqual(runner.commands, want) {
		t.Fatalf("commands = %v, want %v", runner.commands, want)
	}
	after, afterMtime, _ := snapshotFile(t, userConfig)
	afterEntries, _ := os.ReadDir(configDir)
	if after != before || !afterMtime.Equal(beforeMtime) || !reflect.DeepEqual(entryNames(beforeEntries), entryNames(afterEntries)) {
		t.Fatalf("install changed user OpenCode data: content=%q mtime=%v entries=%v", after, afterMtime, entryNames(afterEntries))
	}
	if _, err := os.Stat(filepath.Join(root, ".agw")); !os.IsNotExist(err) {
		t.Fatalf("install created persistent agw state unexpectedly: %v", err)
	}
}

func entryNames(entries []os.DirEntry) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}
