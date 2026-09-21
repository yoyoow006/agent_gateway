package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"agent_gateway/internal/config"
)

// fakeRunner 记录命令，git 存在。
type fakeRunner struct {
	commands []string
	fail     bool
}

func (r *fakeRunner) Run(name string, args ...string) error {
	r.commands = append(r.commands, name)
	if r.fail {
		return &runErr{msg: name + ": not found"}
	}
	return nil
}

type runErr struct{ msg string }

func (e *runErr) Error() string { return e.msg }

func TestValidName(t *testing.T) {
	ok := regexp.MustCompile(validNamePattern)
	for _, name := range []string{"demo", "my-app", "app_2", "a"} {
		if !ok.MatchString(name) {
			t.Errorf("%q 应合法", name)
		}
	}
	for _, name := range []string{"Demo", "-x", "a b", "../evil", ""} {
		if ok.MatchString(name) {
			t.Errorf("%q 应非法", name)
		}
	}
}

func TestNewCreatesAll(t *testing.T) {
	root := t.TempDir()
	r := &fakeRunner{}
	tok, err := New(root, "demo", r)
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) < 10 {
		t.Errorf("令牌 = %q", tok)
	}
	dir := filepath.Join(root, "projects", "demo")
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Fatalf("项目目录未创建: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "agw.toml")); err != nil {
		t.Fatal("agw.toml 模板未生成")
	}
	// git 被调用
	if len(r.commands) == 0 || r.commands[0] != "git" {
		t.Errorf("git 未被调用: %v", r.commands)
	}
	// 令牌写入 local 配置
	data, _ := os.ReadFile(filepath.Join(root, "config", "local.toml"))
	if !regexp.MustCompile(`(?m)^\s*\[projects\.demo\]`).MatchString(string(data)) || !regexp.MustCompile(`token\s*=\s*"agw-`).MatchString(string(data)) {
		t.Errorf("local.toml 缺少项目令牌:\n%s", data)
	}
	// 0600
	fi, _ := os.Stat(filepath.Join(root, "config", "local.toml"))
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("local.toml 权限 = %v", fi.Mode().Perm())
	}
}

func TestNewConflict(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "projects", "demo"), 0o755)
	if _, err := New(root, "demo", &fakeRunner{}); err == nil {
		t.Fatal("已存在应报错")
	}
}

func TestNewInvalidName(t *testing.T) {
	if _, err := New(t.TempDir(), "Bad Name", &fakeRunner{}); err == nil {
		t.Fatal("非法名称应报错")
	}
}

func TestNewGitMissingWarns(t *testing.T) {
	root := t.TempDir()
	if _, err := New(root, "nogit", &fakeRunner{fail: true}); err != nil {
		t.Fatalf("git 缺失应告警跳过而非失败: %v", err)
	}
}

func TestList(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "projects", "alpha"), 0o755)
	os.MkdirAll(filepath.Join(root, "projects", "beta"), 0o755)
	os.WriteFile(filepath.Join(root, "projects", "alpha", "agw.toml"), []byte("[project]\nproviders=[\"x\"]\npreferred=\"x\"\n"), 0o644)
	items, err := List(root, &fakeRunner{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("list = %+v", items)
	}
	var alpha *Item
	for i := range items {
		if items[i].Name == "alpha" {
			alpha = &items[i]
		}
	}
	if alpha == nil || alpha.Preferred != "x" || len(alpha.Providers) != 1 {
		t.Errorf("alpha 覆盖摘要错误: %+v", alpha)
	}
}

func TestNewInvalidConfigLeavesNoProject(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "config"), 0o755)
	os.WriteFile(filepath.Join(root, "config", "default.toml"), []byte("not [valid\n"), 0o644)

	if _, err := New(root, "demo", &fakeRunner{}); err == nil {
		t.Fatal("invalid configuration should fail")
	}
	if _, err := os.Stat(filepath.Join(root, "projects", "demo")); !os.IsNotExist(err) {
		t.Fatalf("project directory should not exist after failure: %v", err)
	}
}

func TestNewRefusesExistingProjectToken(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "config"), 0o755)
	os.WriteFile(filepath.Join(root, "config", "local.toml"), []byte(`
[projects.demo]
token = "agw-existing"
`), 0o600)

	if _, err := New(root, "demo", &fakeRunner{}); err == nil {
		t.Fatal("existing project token should fail")
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Projects["demo"].Token; got != "agw-existing" {
		t.Fatalf("existing token = %q, want agw-existing", got)
	}
	if _, err := os.Stat(filepath.Join(root, "projects", "demo")); !os.IsNotExist(err) {
		t.Fatalf("project directory should not exist: %v", err)
	}
}

func TestNewTokenSaveFailureLeavesNoProject(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "config"), 0o755)
	previous := saveProjectConfig
	saveProjectConfig = func(string, *config.Config) error { return errors.New("disk full") }
	t.Cleanup(func() { saveProjectConfig = previous })
	runner := &fakeRunner{}

	if _, err := New(root, "demo", runner); err == nil {
		t.Fatal("token save failure should fail")
	}
	if _, err := os.Stat(filepath.Join(root, "projects", "demo")); !os.IsNotExist(err) {
		t.Fatalf("project directory should not exist: %v", err)
	}
	if len(runner.commands) != 0 {
		t.Fatalf("git should not run: %v", runner.commands)
	}
}

func TestNewProjectArtifactFailureRollsBackToken(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "config"), 0o755)
	os.MkdirAll(filepath.Join(root, "projects"), 0o755)
	existing := filepath.Join(root, "projects", "demo")
	if err := os.WriteFile(existing, []byte("existing file"), 0o444); err != nil {
		t.Fatal(err)
	}

	_, err := New(root, "demo", &fakeRunner{})
	if err == nil {
		t.Fatal("artifact creation should fail")
	}
	if data, err := os.ReadFile(existing); err != nil || string(data) != "existing file" {
		t.Fatalf("existing path changed: %v %q", err, data)
	}
	data, err := os.ReadFile(filepath.Join(root, "config", "local.toml"))
	if err == nil && strings.Contains(string(data), "[projects.demo]") {
		t.Fatalf("token was not rolled back: %s", data)
	}
}
