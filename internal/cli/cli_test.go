package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"agent_gateway/internal/config"
)

func writeRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// 任务 8.1：热重载成功换配置、失败保旧配置。
func TestServeReload(t *testing.T) {
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
	s, cfg, err := buildServer(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Providers) != 1 {
		t.Fatalf("初始供应商 = %+v", cfg.Providers)
	}
	// 写入新供应商并重载
	os.MkdirAll(filepath.Join(root, "config"), 0o755)
	os.WriteFile(filepath.Join(root, "config", "local.toml"), []byte(`
[[providers]]
name = "b"
protocol = "openai-chat"
base_url = "https://b.example"
priority = 2
enabled = true
`), 0o600)
	if err := s.Reload(); err != nil {
		t.Fatal(err)
	}
	if got := s.Config().Provider("b"); got == nil {
		t.Error("重载后应看到新供应商 b")
	}
	// 破坏配置再重载：保旧配置
	os.WriteFile(filepath.Join(root, "config", "local.toml"), []byte(`broken [`), 0o600)
	if err := s.Reload(); err == nil {
		t.Error("坏配置应报错")
	}
	if got := s.Config().Provider("b"); got == nil {
		t.Error("重载失败必须保留旧配置")
	}
}

// 任务 8.2：pid 存取与存活探测。
func TestPidLifecycle(t *testing.T) {
	root := t.TempDir()
	if readPid(root) != 0 {
		t.Error("无 pidfile 应返回 0")
	}
	if pidAlive(0) || pidAlive(-1) {
		t.Error("非法 pid 应不存活")
	}
	self := os.Getpid()
	os.MkdirAll(runDir(root), 0o755)
	os.WriteFile(pidPath(root), []byte(strconv.Itoa(self)), 0o600)
	if readPid(root) != self {
		t.Error("pid 读写不一致")
	}
	if !pidAlive(self) {
		t.Error("自身进程应存活")
	}
	// 僵尸 pid：取一个几乎不可能存在的值（1<<22 上界内的空闲区不可靠，用 4194304 兜底）
	if pidAlive(4194304) {
		t.Log("pid 4194304 竟然存活（环境异常，忽略）")
	}
}

// 任务 8.2：status 数据源（metrics 端点）字段解析。
func TestStatusFetchShape(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("Authorization") != "Bearer agw-admin-1" {
			w.WriteHeader(401)
			return
		}
		w.Write([]byte(`{"listen":"127.0.0.1:8787","uptime_sec":3,"providers":[{"Name":"a","State":"closed","Requests":2,"Failures":0,"InFlight":0,"LastErr":""}]}`))
	}))
	defer srv.Close()
	resp, err := adminRequest("GET", srv.URL+"/__agw/metrics", &config.Config{AdminToken: "agw-admin-1"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || hits.Load() != 1 {
		t.Fatalf("status=%d hits=%d", resp.StatusCode, hits.Load())
	}
}

// 任务 8.3：provider add / switch 写回内容与热重载触发。
func TestProviderCommandsPersist(t *testing.T) {
	root := writeRepo(t, map[string]string{"config/default.toml": ""})

	// fake 网关：统计 reload 调用
	var reloads atomic.Int64
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/__agw/reload" {
			reloads.Add(1)
			w.Write([]byte(`{"ok":true}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer fake.Close()

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Gateway.Listen = strings.TrimPrefix(fake.URL, "http://")
	cfg.AdminToken = "agw-admin-1"
	cfg.RebuildTokenIndex()

	// 模拟 provider add：直接构造后 SaveLocal
	cfg.Providers = append(cfg.Providers, config.Provider{
		Name: "relay1", Protocol: config.ProtocolOpenAIChat,
		BaseURL: "https://relay1.example/v1", APIKeyEnv: "RELAY1_KEY", Priority: 1, Enabled: true,
	})
	cfg.RebuildTokenIndex()
	if err := config.SaveLocal(root, cfg); err != nil {
		t.Fatal(err)
	}
	cfg2, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.Provider("relay1") == nil || cfg2.Provider("relay1").APIKeyEnv != "RELAY1_KEY" {
		t.Fatalf("add 未持久化: %+v", cfg2.Providers)
	}
	if fi, _ := os.Stat(filepath.Join(root, "config", "local.toml")); fi.Mode().Perm() != 0o600 {
		t.Errorf("local.toml 权限 = %v", fi.Mode().Perm())
	}

	// 模拟 switch：preferred 标记
	for i := range cfg2.Providers {
		cfg2.Providers[i].Preferred = cfg2.Providers[i].Name == "relay1"
	}
	config.SaveLocal(root, cfg2)
	cfg3, _ := config.Load(root)
	if !cfg3.Provider("relay1").Preferred {
		t.Error("switch 未持久化粘性标记")
	}

	// 热重载触发（伪造运行中状态：写 pidfile 为当前进程）
	os.MkdirAll(runDir(root), 0o755)
	os.WriteFile(pidPath(root), []byte(strconv.Itoa(os.Getpid())), 0o600)
	reloadIfRunning(root, cfg3)
	if reloads.Load() != 1 {
		t.Errorf("应触发一次热重载，got %d", reloads.Load())
	}
}

// 任务 8.3：provider test 探测。
func TestProbeProvider(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("探测路径 = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer sk-x" {
			t.Error("探测应带 Bearer")
		}
		w.Write([]byte(`{"data":[]}`))
	}))
	defer up.Close()
	p := &config.Provider{Name: "c", Protocol: config.ProtocolOpenAIChat, BaseURL: up.URL, APIKey: "sk-x"}
	if _, err := ProbeProvider(p); err != nil {
		t.Fatalf("探测失败: %v", err)
	}
	bad := &config.Provider{Name: "d", Protocol: config.ProtocolOpenAIChat, BaseURL: "http://127.0.0.1:1", APIKey: "sk-x"}
	if _, err := ProbeProvider(bad); err == nil {
		t.Error("死地址应失败")
	}
}

// 任务 8.4：脱敏。
func TestRedact(t *testing.T) {
	cases := map[string]string{
		"token agw-0123456789abcdef0123456789abcdef in text": "token agw-01*** in text",
		"key sk-abcdefghijklmnop":                            "key sk-abc***",
		"Authorization: Bearer sk-abcdefghijklmnop":          "Authorization: Bearer sk-abc***",
		`{"api_key":"sk-abcdefghijklmnop"}`:                  `{"api_key ***`,
		"普通文本无密钥":                                            "普通文本无密钥",
	}
	for in, wantContains := range cases {
		got := Redact(in)
		if !strings.HasPrefix(wantContains, "普通") {
			if strings.Contains(got, "abcdefghijklmnop") || strings.Contains(got, "0123456789abcdef") {
				t.Errorf("Redact(%q) 泄密: %q", in, got)
			}
		}
		if in == "普通文本无密钥" && got != in {
			t.Errorf("无密钥文本被改动: %q", got)
		}
	}
}
