package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// snapshotFile 记录文件内容与修改时间（零接触断言用）；不存在返回标记。
func snapshotFile(t *testing.T, path string) (string, time.Time, bool) {
	fi, err := os.Stat(path)
	if err != nil {
		return "", time.Time{}, false
	}
	data, _ := os.ReadFile(path)
	return string(data), fi.ModTime(), true
}

// 任务1.2：GenerateClaudeSettings。
func TestGenerateClaudeSettings(t *testing.T) {
	root := t.TempDir()
	path, err := GenerateClaudeSettings(root, "foo", "127.0.0.1:8787", "agw-foo-tok")
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(root, ".agw", "claude-settings.foo.json") {
		t.Fatalf("path = %s", path)
	}
	var settings map[string]any
	data, _ := os.ReadFile(path)
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	env, _ := settings["env"].(map[string]any)
	if env["ANTHROPIC_BASE_URL"] != "http://127.0.0.1:8787" || env["ANTHROPIC_AUTH_TOKEN"] != "agw-foo-tok" {
		t.Fatalf("env = %+v", env)
	}
	fi, _ := os.Stat(path)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("权限 = %v", fi.Mode().Perm())
	}
	// global 命名
	p2, _ := GenerateClaudeSettings(root, "", "127.0.0.1:8787", "agw-g")
	if !strings.HasSuffix(p2, "claude-settings.global.json") {
		t.Errorf("global 命名 = %s", p2)
	}
	// 重写：换令牌后重跑同名文件更新
	GenerateClaudeSettings(root, "foo", "127.0.0.1:8787", "agw-new-tok")
	data, _ = os.ReadFile(path)
	if !strings.Contains(string(data), "agw-new-tok") {
		t.Error("重写未生效")
	}
}

// 任务1.2：EnsureCodexProfile。
func TestEnsureCodexProfile(t *testing.T) {
	home := t.TempDir()
	codexHome := filepath.Join(home, ".codex")
	if err := EnsureCodexProfile(codexHome, "127.0.0.1:8787"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(codexHome, "agw.config.toml")
	var cfg map[string]any
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["model_provider"] != "agw" {
		t.Errorf("model_provider = %v", cfg["model_provider"])
	}
	if v, _ := cfg["disable_response_storage"].(bool); !v {
		t.Error("disable_response_storage 应为 true")
	}
	provs, _ := cfg["model_providers"].(map[string]any)
	agw, _ := provs["agw"].(map[string]any)
	if agw == nil || agw["base_url"] != "http://127.0.0.1:8787/v1" || agw["wire_api"] != "responses" || agw["env_key"] != "AGW_API_KEY" {
		t.Fatalf("agw provider = %+v", agw)
	}
	fi, _ := os.Stat(path)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("权限 = %v", fi.Mode().Perm())
	}
	// config.toml 未被创建（零接触）
	if _, err := os.Stat(filepath.Join(codexHome, "config.toml")); err == nil {
		t.Error("config.toml 不应被创建")
	}
	// 幂等重写：换网关地址后重跑更新
	if err := EnsureCodexProfile(codexHome, "127.0.0.1:9999"); err != nil {
		t.Fatal(err)
	}
	var cfg2 map[string]any
	toml.DecodeFile(path, &cfg2)
	provs2, _ := cfg2["model_providers"].(map[string]any)
	agw2, _ := provs2["agw"].(map[string]any)
	if agw2["base_url"] != "http://127.0.0.1:9999/v1" {
		t.Errorf("幂等重写失败: %+v", agw2)
	}
}

// 任务1.2：install 全程零接触用户默认配置。
func TestInstallZeroTouch(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	// 预置用户既有配置（含自定义内容）
	claudeDir := filepath.Join(home, ".claude")
	os.MkdirAll(claudeDir, 0o755)
	os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(`{"theme":"dark"}`), 0o644)
	codexDir := filepath.Join(home, ".codex")
	os.MkdirAll(codexDir, 0o755)
	os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte("model = \"gpt-5.2\"\n"), 0o644)

	cs, cm, _ := snapshotFile(t, filepath.Join(claudeDir, "settings.json"))
	xs, xm, _ := snapshotFile(t, filepath.Join(codexDir, "config.toml"))

	runner := &okRunner{}
	if err := InstallClaude(Options{Home: home, Root: root, Listen: "127.0.0.1:8787", Runner: runner}); err != nil {
		t.Fatal(err)
	}
	if err := InstallCodex(Options{Home: home, Root: root, Listen: "127.0.0.1:8787", Runner: runner}); err != nil {
		t.Fatal(err)
	}
	// npm 各两条（探测+安装）
	if len(runner.commands) != 4 {
		t.Fatalf("npm 命令 = %v", runner.commands)
	}
	// 用户默认配置内容与 mtime 不变
	cs2, cm2, _ := snapshotFile(t, filepath.Join(claudeDir, "settings.json"))
	if cs2 != cs || !cm2.Equal(cm) {
		t.Errorf("claude settings 被改动: %q→%q", cs, cs2)
	}
	xs2, xm2, _ := snapshotFile(t, filepath.Join(codexDir, "config.toml"))
	if xs2 != xs || !xm2.Equal(xm) {
		t.Errorf("codex config 被改动: %q→%q", xs, xs2)
	}
	// 独立产物就位
	if fi, err := os.Stat(filepath.Join(root, ".agw")); err != nil || !fi.IsDir() {
		t.Errorf(".agw 目录未创建: %v", err)
	}
	if _, err := os.Stat(filepath.Join(codexDir, "agw.config.toml")); err != nil {
		t.Errorf("codex profile 未生成: %v", err)
	}
}

func TestInstallNpmMissing(t *testing.T) {
	err := InstallClaude(Options{Home: t.TempDir(), Root: t.TempDir(), Listen: "127.0.0.1:8787", Runner: failRunner{}})
	if err == nil || !strings.Contains(err.Error(), "npm") {
		t.Fatalf("应给出 npm 指引: %v", err)
	}
}

// 任务1.4：PrepareExec 注入独立配置参数。
func TestPrepareExecIsolatedConfigs(t *testing.T) {
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
	t.Setenv("HOME", home) // 隔离 ~/.codex

	// claude：argv 含 --settings，文件含项目令牌，令牌不再直接注入进程 env
	env, dir, argv, err := PrepareExec(root, "claude", "foo", []string{"--model", "x"})
	if err != nil {
		t.Fatal(err)
	}
	if len(argv) < 4 || argv[0] != "claude" || argv[1] != "--settings" {
		t.Fatalf("argv = %v", argv)
	}
	settingsPath := argv[2]
	if !strings.Contains(settingsPath, ".agw") {
		t.Fatalf("settings 路径 = %s", settingsPath)
	}
	data, _ := os.ReadFile(settingsPath)
	if !strings.Contains(string(data), "agw-foo") {
		t.Fatalf("settings 未含项目令牌: %s", data)
	}
	if env["ANTHROPIC_AUTH_TOKEN"] != "" {
		t.Errorf("claude 令牌应走 settings 文件而非进程 env: %v", env)
	}
	if dir != filepath.Join(root, "projects", "foo") {
		t.Errorf("dir = %s", dir)
	}
	if argv[len(argv)-1] != "x" || argv[len(argv)-2] != "--model" {
		t.Errorf("用户参数应在末尾: %v", argv)
	}

	// codex：argv 含 -p agw，env 含 AGW_API_KEY，profile 已确保
	codexHome := filepath.Join(t.TempDir(), "codex-home")
	t.Setenv("CODEX_HOME", codexHome)
	env, _, argv, err = PrepareExec(root, "codex", "foo", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(argv) < 3 || argv[0] != "codex" || argv[1] != "-p" || argv[2] != "agw" {
		t.Fatalf("argv = %v", argv)
	}
	if env["AGW_API_KEY"] != "agw-foo" {
		t.Errorf("AGW_API_KEY = %v", env)
	}
	if _, err := os.Stat(filepath.Join(codexHome, "agw.config.toml")); err != nil {
		t.Errorf("profile 未确保: %v", err)
	}

	// 项目不存在
	if _, _, _, err := PrepareExec(root, "claude", "nope", nil); err == nil {
		t.Fatal("应报错")
	}
}
