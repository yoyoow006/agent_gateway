package config

import "testing"

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
