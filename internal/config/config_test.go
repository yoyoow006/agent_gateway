package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeRepo 写一个最小网关仓库目录结构并返回其根。
func writeRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestLoadMergePrecedence(t *testing.T) {
	root := writeRepo(t, map[string]string{
		"config/default.toml": `
[gateway]
listen = "127.0.0.1:9000"

[[providers]]
name = "official"
protocol = "anthropic"
base_url = "https://api.anthropic.com"
priority = 10
enabled = true

[[providers]]
name = "relay1"
protocol = "openai-chat"
base_url = "https://relay1.example/v1"
priority = 1
enabled = true
`,
		"config/local.toml": `
[[providers]]
name = "relay1"
base_url = "https://relay1-new.example/v1"
priority = 5
`,
	})
	cfg, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Gateway.Listen != "127.0.0.1:9000" {
		t.Errorf("listen = %q, want 127.0.0.1:9000", cfg.Gateway.Listen)
	}
	relay := cfg.Provider("relay1")
	if relay == nil {
		t.Fatal("relay1 missing after merge")
	}
	// local 覆盖 base_url/priority，default 里的 protocol/enabled 保留。
	if relay.BaseURL != "https://relay1-new.example/v1" {
		t.Errorf("base_url = %q, want overridden", relay.BaseURL)
	}
	if relay.Priority != 5 {
		t.Errorf("priority = %d, want 5", relay.Priority)
	}
	if relay.Protocol != ProtocolOpenAIChat {
		t.Errorf("protocol = %q, want openai-chat kept from default", relay.Protocol)
	}
	if !relay.Enabled {
		t.Errorf("enabled should be kept true from default")
	}
	if cfg.Provider("official") == nil {
		t.Error("official provider missing")
	}
}

func TestProjectOverrideLimitsChain(t *testing.T) {
	root := writeRepo(t, map[string]string{
		"config/default.toml": `
[[providers]]
name = "a"
protocol = "anthropic"
base_url = "https://a.example"
priority = 1
enabled = true

[[providers]]
name = "b"
protocol = "anthropic"
base_url = "https://b.example"
priority = 2
enabled = true
`,
		"projects/foo/agw.toml": `
[project]
providers = ["b"]
preferred = "b"
[project.model_map]
"claude-x" = "claude-y"
`,
	})
	cfg, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	prof, err := cfg.ResolveProfile("foo")
	if err != nil {
		t.Fatalf("ResolveProfile: %v", err)
	}
	if len(prof.Chain) != 1 || prof.Chain[0].Name != "b" {
		t.Errorf("chain = %+v, want only b", prof.Chain)
	}
	if got := prof.ModelMap["claude-x"]; got != "claude-y" {
		t.Errorf("model_map merge failed: %q", got)
	}
	if prof.Preferred != "b" {
		t.Errorf("preferred = %q, want b", prof.Preferred)
	}
	// 全局档案：无项目覆盖时按优先级排序且全部启用。
	global, err := cfg.ResolveProfile("")
	if err != nil {
		t.Fatal(err)
	}
	if len(global.Chain) != 2 || global.Chain[0].Name != "a" {
		t.Errorf("global chain = %+v", global.Chain)
	}
}

func TestTokenIndex(t *testing.T) {
	root := writeRepo(t, map[string]string{
		"config/local.toml": `
[gateway]
default_token = "agw-global-token"

[projects.foo]
token = "agw-foo-token"
`,
	})
	cfg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if proj, ok := cfg.TokenProject("agw-global-token"); !ok || proj != "" {
		t.Errorf("global token -> %q,%v", proj, ok)
	}
	if proj, ok := cfg.TokenProject("agw-foo-token"); !ok || proj != "foo" {
		t.Errorf("foo token -> %q,%v", proj, ok)
	}
	if _, ok := cfg.TokenProject("nope"); ok {
		t.Error("unknown token should not resolve")
	}
}

func TestResolveAPIKeyEnv(t *testing.T) {
	t.Setenv("RELAY1_KEY", "sk-live")
	p := &Provider{APIKeyEnv: "RELAY1_KEY"}
	k, err := p.ResolveAPIKey()
	if err != nil || k != "sk-live" {
		t.Errorf("ResolveAPIKey = %q,%v", k, err)
	}
	p2 := &Provider{APIKeyEnv: "MISSING_KEY_X"}
	if _, err := p2.ResolveAPIKey(); err == nil {
		t.Error("missing env should error")
	}
	p3 := &Provider{APIKey: "literal"}
	if k, _ := p3.ResolveAPIKey(); k != "literal" {
		t.Errorf("literal key = %q", k)
	}
}

func TestSaveLocalPermissionsAndRoundTrip(t *testing.T) {
	root := writeRepo(t, map[string]string{
		"config/default.toml": `
[[providers]]
name = "a"
protocol = "anthropic"
base_url = "https://a.example"
priority = 1
enabled = true
`,
	})
	cfg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.AdminToken = "agw-admin-x"
	cfg.Gateway.DefaultToken = "agw-default-y"
	cfg.Projects["foo"] = ProjectProfile{Token: "agw-foo-z"}
	if err := SaveLocal(root, cfg); err != nil {
		t.Fatalf("SaveLocal: %v", err)
	}
	fi, err := os.Stat(filepath.Join(root, "config", "local.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("local.toml perm = %v, want 0600", fi.Mode().Perm())
	}
	cfg2, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.AdminToken != "agw-admin-x" || cfg2.Gateway.DefaultToken != "agw-default-y" {
		t.Errorf("round trip lost tokens: %+v", cfg2.Gateway)
	}
	if cfg2.Projects["foo"].Token != "agw-foo-z" {
		t.Errorf("project token lost: %+v", cfg2.Projects)
	}
	if cfg2.Provider("a") == nil {
		t.Error("provider a lost after SaveLocal+Load (local.toml 不含 provider 不应清空 default)")
	}
}

func TestNewTokenFormat(t *testing.T) {
	tok := NewToken()
	if !strings.HasPrefix(tok, "agw-") || len(tok) != len("agw-")+64 {
		t.Errorf("token = %q, want agw-+64hex", tok)
	}
	tok2 := NewToken()
	if tok == tok2 {
		t.Error("tokens should be unique")
	}
}

func TestLoadInvalidTOML(t *testing.T) {
	root := writeRepo(t, map[string]string{
		"config/local.toml": `not [valid toml`,
	})
	if _, err := Load(root); err == nil {
		t.Error("invalid toml should fail")
	}
}

func TestFindRoot(t *testing.T) {
	root := writeRepo(t, map[string]string{"config/default.toml": ""})
	sub := filepath.Join(root, "projects", "foo")
	if got, err := FindRoot(sub); err != nil || got != root {
		t.Errorf("FindRoot(sub) = %q,%v want %q", got, err, root)
	}
	if _, err := FindRoot(t.TempDir()); err == nil {
		t.Error("FindRoot outside repo should error")
	}
}
