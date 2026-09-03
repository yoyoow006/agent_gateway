package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func writeRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(content), 0o644)
	}
	return root
}

// failRunner 模拟缺失的命令。
type failRunner struct{}

func (failRunner) Run(name string, args ...string) error {
	return &execError{msg: name + ": command not found"}
}

type execError struct{ msg string }

func (e *execError) Error() string { return e.msg }

// okRunner 模拟 npm 成功。
type okRunner struct{ commands []string }

func (r *okRunner) Run(name string, args ...string) error {
	r.commands = append(r.commands, name+" "+strings.Join(args, " "))
	return nil
}

func TestInstallClaudeSettingsMerge(t *testing.T) {
	home := t.TempDir()
	claudeDir := filepath.Join(home, ".claude")
	os.MkdirAll(claudeDir, 0o755)
	// 用户已有自定义配置
	os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(`{
		"theme": "dark",
		"permissions": {"allow": ["Bash(ls)"]},
		"env": {"OTHER": "keep"}
	}`), 0o644)

	runner := &okRunner{}
	if err := InstallClaude(Options{Home: home, Listen: "127.0.0.1:8787", Token: "agw-tok", Runner: runner}); err != nil {
		t.Fatal(err)
	}
	// npm 被调用（探测 + 安装两条）
	if len(runner.commands) != 2 || !strings.Contains(runner.commands[1], "@anthropic-ai/claude-code") {
		t.Errorf("npm 命令 = %v", runner.commands)
	}
	// 备份存在
	backups, _ := filepath.Glob(filepath.Join(claudeDir, "settings.json.agw-backup-*"))
	if len(backups) != 1 {
		t.Fatalf("应生成一个备份, got %v", backups)
	}
	var settings map[string]any
	data, _ := os.ReadFile(filepath.Join(claudeDir, "settings.json"))
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	// 用户键保留
	if settings["theme"] != "dark" {
		t.Error("用户键 theme 被破坏")
	}
	env, _ := settings["env"].(map[string]any)
	if env["OTHER"] != "keep" {
		t.Error("用户 env 键被破坏")
	}
	if env["ANTHROPIC_BASE_URL"] != "http://127.0.0.1:8787" {
		t.Errorf("BASE_URL = %v", env["ANTHROPIC_BASE_URL"])
	}
	if env["ANTHROPIC_AUTH_TOKEN"] != "agw-tok" {
		t.Errorf("AUTH_TOKEN = %v", env["ANTHROPIC_AUTH_TOKEN"])
	}
}

func TestInstallClaudeCorruptedJSONRefuses(t *testing.T) {
	home := t.TempDir()
	claudeDir := filepath.Join(home, ".claude")
	os.MkdirAll(claudeDir, 0o755)
	os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(`{broken`), 0o644)
	err := InstallClaude(Options{Home: home, Listen: "127.0.0.1:8787", Token: "t", Runner: &okRunner{}})
	if err == nil {
		t.Fatal("损坏 JSON 应拒绝写入")
	}
	data, _ := os.ReadFile(filepath.Join(claudeDir, "settings.json"))
	if string(data) != `{broken` {
		t.Error("原文件被改动")
	}
}

func TestInstallCodexConfigMerge(t *testing.T) {
	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	os.MkdirAll(codexDir, 0o755)
	os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte(`
model = "gpt-5.2"

[model_providers.mine]
name = "Mine"
base_url = "https://mine.example/v1"
env_key = "MINE_KEY"
`), 0o644)

	if err := InstallCodex(Options{Home: home, Listen: "127.0.0.1:8787", Token: "agw-tok", Runner: &okRunner{}}); err != nil {
		t.Fatal(err)
	}
	backups, _ := filepath.Glob(filepath.Join(codexDir, "config.toml.agw-backup-*"))
	if len(backups) != 1 {
		t.Fatalf("应生成一个备份, got %v", backups)
	}
	var cfg map[string]any
	if _, err := toml.DecodeFile(filepath.Join(codexDir, "config.toml"), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["model"] != "gpt-5.2" {
		t.Error("用户 model 键被破坏")
	}
	if cfg["model_provider"] != "agw" {
		t.Errorf("model_provider = %v", cfg["model_provider"])
	}
	if v, _ := cfg["disable_response_storage"].(bool); !v {
		t.Error("应禁用响应存储（无状态可切换前提）")
	}
	provs, _ := cfg["model_providers"].(map[string]any)
	agw, _ := provs["agw"].(map[string]any)
	if agw == nil {
		t.Fatalf("model_providers.agw 缺失: %+v", provs)
	}
	if agw["base_url"] != "http://127.0.0.1:8787/v1" {
		t.Errorf("base_url = %v", agw["base_url"])
	}
	if agw["wire_api"] != "responses" {
		t.Errorf("wire_api = %v", agw["wire_api"])
	}
	if agw["env_key"] != "AGW_API_KEY" {
		t.Errorf("env_key = %v", agw["env_key"])
	}
	mine, _ := provs["mine"].(map[string]any)
	if mine == nil || mine["base_url"] != "https://mine.example/v1" {
		t.Error("用户 provider 被破坏")
	}
}

func TestInstallNpmMissing(t *testing.T) {
	err := InstallClaude(Options{Home: t.TempDir(), Listen: "127.0.0.1:8787", Token: "t", Runner: failRunner{}})
	if err == nil || !strings.Contains(err.Error(), "npm") {
		t.Fatalf("应给出 npm 指引: %v", err)
	}
}

func TestPrepareExec(t *testing.T) {
	root := writeRepo(t, map[string]string{
		"config/local.toml": `
[gateway]
default_token = "agw-global"

[projects.foo]
token = "agw-foo"
`,
		"projects/foo/.keep": "",
	})
	// claude 项目令牌
	env, dir, argv, err := PrepareExec(root, "claude", "foo", []string{"--model", "x"})
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join(root, "projects", "foo") {
		t.Errorf("dir = %s", dir)
	}
	if env["ANTHROPIC_BASE_URL"] != "http://127.0.0.1:8787" || env["ANTHROPIC_AUTH_TOKEN"] != "agw-foo" {
		t.Errorf("claude env = %v", env)
	}
	if len(argv) != 3 || argv[0] != "claude" || argv[2] != "x" {
		t.Errorf("argv = %v", argv)
	}
	// codex 全局令牌（无项目 → cwd 不在 projects 下）
	env, _, argv, err = PrepareExec(root, "codex", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if env["AGW_API_KEY"] != "agw-global" {
		t.Errorf("codex env = %v", env)
	}
	if len(argv) != 1 || argv[0] != "codex" {
		t.Errorf("argv = %v", argv)
	}
	// 项目不存在
	_, _, _, err = PrepareExec(root, "claude", "nope", nil)
	if err == nil || !strings.Contains(err.Error(), "foo") {
		t.Fatalf("应列出可用项目: %v", err)
	}
}
