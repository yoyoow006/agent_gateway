package config

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// 任务2.3 验证：default.toml 单独可加载且不含明文密钥字段。
func TestDefaultTOMLLoadable(t *testing.T) {
	cfg, err := Load(".")
	if err != nil {
		t.Fatalf("Load(default only): %v", err)
	}
	if cfg.Gateway.Listen != "127.0.0.1:8787" {
		t.Errorf("listen = %q", cfg.Gateway.Listen)
	}
	for _, p := range cfg.Providers {
		if p.APIKey != "" {
			t.Errorf("provider %s 在 default.toml 中含明文 api_key", p.Name)
		}
	}
}

func TestDocumentationProviderBaseURLsOmitV1Path(t *testing.T) {
	for _, path := range []string{"../../README.md", "../../docs/usage-guide.md"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		text := string(data)
		for _, match := range regexp.MustCompile(`(?i)(?:--base-url\s+|base_url\s*=\s*")https://[^"[:space:]]+/v1`).FindAllString(text, -1) {
			t.Errorf("%s: provider base_url example must omit /v1: %s", path, match)
		}
	}
}

func TestDocumentationRoutingSemanticsMatchRuntime(t *testing.T) {
	read := func(path string) string {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(data)
	}
	for _, path := range []string{"../../README.md", "../../docs/usage-guide.md"} {
		text := read(path)
		if strings.Contains(text, "providers = [\"relay\"]       # 只用这几家（按此顺序）") {
			t.Errorf("%s: providers list must not be documented as routing order", path)
		}
		if !strings.Contains(text, "401、403、408、429、500、502、503、504、529") {
			t.Errorf("%s: exact failover status list is missing", path)
		}
		if strings.Contains(text, "408 / 429 / 5xx / 529") {
			t.Errorf("%s: generic 5xx failover wording must be replaced with the exact list", path)
		}
	}
	usage := read("../../docs/usage-guide.md")
	if !strings.Contains(usage, "需重启网关后生效") {
		t.Error("usage guide must document that provider timeout changes require gateway restart")
	}
	template := read("../workspace/project.go")
	if strings.Contains(template, "按序启用的供应商子集") {
		t.Error("workspace project template must not claim providers list controls order")
	}
	if !strings.Contains(template, "候选子集") || !strings.Contains(template, "priority") {
		t.Error("workspace project template must document subset and priority ordering")
	}
}
