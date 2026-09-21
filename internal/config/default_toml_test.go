package config

import (
	"os"
	"path/filepath"
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

func TestDocumentationAndPackageReferencesResolve(t *testing.T) {
	roots := []string{"../..", "."}
	for _, path := range []string{"../../README.md", "../../docs/usage-guide.md", "../../docs/protocol-flow.md"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if strings.Contains(string(data), "openspec/archive/add-agent-gateway/design.md") {
			t.Errorf("%s references missing add-agent-gateway archive", path)
		}
		if strings.Contains(string(data), "openspec/archive/translate-additional-tools/") {
			t.Errorf("%s references missing translate-additional-tools archive", path)
		}
	}
	docFiles, err := filepath.Glob("../*/doc.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(docFiles) == 0 {
		t.Fatal("expected package doc files")
	}
	for _, doc := range docFiles {
		data, err := os.ReadFile(doc)
		if err != nil {
			t.Fatalf("read %s: %v", doc, err)
		}
		text := string(data)
		if strings.Contains(text, "openspec/changes/add-agent-gateway/design.md") {
			t.Errorf("%s references missing active OpenSpec change", doc)
		}
		if !strings.Contains(text, "openspec/specs/") {
			t.Errorf("%s lacks a current OpenSpec spec reference", doc)
		}
	}
	if !strings.Contains(mustRead(t, "../../README.md"), "Go ≥1.24.11") {
		t.Error("README must document the go.mod minimum 1.24.11")
	}
	_ = roots
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestDocumentationProviderHeadersCannotOverrideAuth(t *testing.T) {
	for _, path := range []string{"../../config/default.toml", "../../README.md", "../../docs/usage-guide.md"} {
		text := mustRead(t, path)
		if !strings.Contains(text, "不会覆盖网关注入的认证头") {
			t.Errorf("%s lacks provider headers cannot override auth guidance", path)
		}
	}
}

func TestDocumentationLocalClientErrors(t *testing.T) {
	for _, path := range []string{"../../README.md", "../../docs/protocol-flow.md"} {
		text := mustRead(t, path)
		if !strings.Contains(text, "本地 400") || !strings.Contains(text, "不计供应商失败") {
			t.Errorf("%s lacks local 400 and provider-metric isolation guidance", path)
		}
	}
}

func TestDocumentationRootFlagUsesSubcommandPosition(t *testing.T) {
	for _, path := range []string{"../../README.md", "../../docs/usage-guide.md"} {
		text := mustRead(t, path)
		if strings.Contains(text, "agw --root") {
			t.Errorf("%s documents --root before subcommand, which cobra rejects", path)
		}
	}
	usage := mustRead(t, "../../docs/usage-guide.md")
	if !strings.Contains(usage, "agw <cmd> --root <dir>") || !strings.Contains(usage, "agw start --root /path/to/agent_gateway") {
		t.Error("usage guide must show root flag after subcommand")
	}
}

func TestDocumentationProcessIdentityAndReadiness(t *testing.T) {
	readme := mustRead(t, "../../README.md")
	usage := mustRead(t, "../../docs/usage-guide.md")
	if !strings.Contains(readme, "healthz 就绪后写身份 pidfile") || !strings.Contains(readme, "身份匹配后优雅停止") {
		t.Error("README lacks process identity and readiness semantics")
	}
	if !strings.Contains(usage, "healthz 就绪后写身份 pidfile") || !strings.Contains(usage, "校验 PID 身份") || !strings.Contains(usage, "旧纯 PID pidfile 拒绝发信号") {
		t.Error("usage guide lacks process identity and readiness semantics")
	}
}

func TestDocumentationIPv6ListenFormat(t *testing.T) {
	for _, path := range []string{"../../README.md", "../../docs/usage-guide.md"} {
		text := mustRead(t, path)
		if !strings.Contains(text, "[::1]:8787") {
			t.Errorf("%s lacks bracketed IPv6 listen guidance", path)
		}
	}
}

func TestDocumentationModelsAndLogLevelSemantics(t *testing.T) {
	readme := mustRead(t, "../../README.md")
	usage := mustRead(t, "../../docs/usage-guide.md")
	for _, text := range []string{readme, usage} {
		if !strings.Contains(text, "via-<供应商名>") || !strings.Contains(text, "default_model") {
			t.Error("docs must explain local /v1/models mapping summary semantics")
		}
	}
	if strings.Contains(mustRead(t, "../../config/default.toml"), "log_level =") {
		t.Error("default config must not suggest ineffective log_level")
	}
	if !strings.Contains(usage, "log_level") || !strings.Contains(usage, "不生效") {
		t.Error("usage guide must disclose ineffective log_level")
	}
}
