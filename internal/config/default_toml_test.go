package config

import (
	"os"
	"regexp"
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
