package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

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
	if _, err := reloadIfRunning(root, cfg3); err != nil {
		t.Fatalf("provider 路径 reload 不应失败: %v", err)
	}
	if reloads.Load() != 1 {
		t.Errorf("应触发一次热重载，got %d", reloads.Load())
	}
}

func TestProviderAddDefaultModel(t *testing.T) {
	root := writeRepo(t, map[string]string{"config/default.toml": ""})
	cmd := NewRootCmd()
	cmd.SetArgs([]string{
		"provider", "add", "relay", "--root", root,
		"--protocol", "openai-chat", "--base-url", "https://relay.example",
		"--api-key-env", "RELAY_KEY", "--priority", "1",
		"--default-model", "gpt-safe",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("provider add: %v", err)
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Provider("relay").DefaultModel; got != "gpt-safe" {
		t.Fatalf("default_model = %q, want gpt-safe", got)
	}

	cmd = NewRootCmd()
	cmd.SetArgs([]string{
		"provider", "add", "relay", "--root", root,
		"--protocol", "openai-chat", "--base-url", "https://relay.example",
		"--api-key-env", "RELAY_KEY",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("provider add update: %v", err)
	}
	cfg, err = config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Provider("relay").DefaultModel; got != "" {
		t.Fatalf("updated default_model = %q, want replacement semantics to clear it", got)
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

// FWD-01 回归：`--` 之后的参数不被 cobra 参数校验拦截。
func TestRunDashDashPassthrough(t *testing.T) {
	root := writeRepo(t, map[string]string{
		"config/local.toml":   "[gateway]\ndefault_token = \"agw-g\"\n",
		"projects/demo/.keep": "",
	})
	var captured struct {
		called bool
		argv   []string
		env    map[string]string
		dir    string
	}
	prev := execAgent
	execAgent = func(env map[string]string, dir string, argv []string) error {
		captured.called, captured.argv, captured.env, captured.dir = true, argv, env, dir
		return nil
	}
	defer func() { execAgent = prev }()

	// 模拟 os.Args 含 `--`（extractAgentArgs 依据 os.Args）
	oldArgs := os.Args
	os.Args = []string{"agw", "run", "--root", root, "claude", "--project", "demo", "--", "--model", "x"}
	defer func() { os.Args = oldArgs }()

	cmd := NewRootCmd()
	cmd.SetArgs([]string{"run", "--root", root, "claude", "--project", "demo", "--", "--model", "x"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("命令应执行成功: %v", err)
	}
	if !captured.called {
		t.Fatal("execAgent 未被调用（参数校验拦截或提前失败）")
	}
	// 独立配置模式：argv = claude --settings <path> --model x；令牌在 settings 文件里
	argv := captured.argv
	if len(argv) != 5 || argv[0] != "claude" || argv[1] != "--settings" ||
		argv[3] != "--model" || argv[4] != "x" || !strings.Contains(argv[2], ".agw") {
		t.Fatalf("argv = %v", argv)
	}
	settingsData, err := os.ReadFile(argv[2])
	if err != nil || !strings.Contains(string(settingsData), "agw-g") {
		t.Errorf("settings 文件应含项目令牌: %v %s", err, settingsData)
	}
	if captured.env["ANTHROPIC_AUTH_TOKEN"] != "" {
		t.Errorf("令牌不应进进程 env: %v", captured.env)
	}
}

// LFC-01 回归：启动失败（子进程秒退）必须报错而非误报成功。
func TestStartGatewayDetectsImmediateExit(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(runDir(root), 0o755)
	// 崩溃脚本：写一行"日志"然后退出 1
	failScript := filepath.Join(root, "fail.sh")
	os.WriteFile(failScript, []byte("#!/bin/sh\necho 'listen tcp: bind: address already in use'\nexit 1\n"), 0o755)
	prev := selfPath
	selfPath = func() (string, error) { return failScript, nil }
	defer func() { selfPath = prev }()

	_, err := StartGateway(root, "127.0.0.1:8787")
	if err == nil {
		t.Fatal("子进程秒退必须报错")
	}
	if !strings.Contains(err.Error(), "address already in use") {
		t.Errorf("错误应含日志尾部（占用原因）: %v", err)
	}
	if _, statErr := os.Stat(pidPath(root)); statErr == nil {
		t.Error("失败后 pidfile 应被清理")
	}
}

// LFC-01 补充：正常存活路径仍报成功并清理。
func TestStartGatewayAlivePath(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(runDir(root), 0o755)
	okScript := filepath.Join(root, "alive.sh")
	os.WriteFile(okScript, []byte("#!/bin/sh\nsleep 30\n"), 0o755)
	prev := selfPath
	prevReady := healthReady
	selfPath = func() (string, error) { return okScript, nil }
	healthReady = func(string, string) bool { return true }
	defer func() { selfPath = prev; healthReady = prevReady }()

	pid, err := StartGateway(root, "127.0.0.1:8787")
	if err != nil {
		t.Fatalf("存活路径应成功: %v", err)
	}
	defer func() {
		if p, e := os.FindProcess(pid); e == nil {
			p.Signal(syscall.SIGKILL)
		}
	}()
	identity, isJSON, err := readPidIdentity(root)
	if err != nil || !isJSON || identity.Pid != pid || identity.Root != root {
		t.Errorf("pidfile identity = %+v json:%v err:%v want pid=%d root=%s", identity, isJSON, err, pid, root)
	}
}

// MIN-02 回归：anthropic 探测带 anthropic-version。
func TestProbeAnthropicVersionHeader(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Anthropic-Version") == "" {
			t.Error("探测请求缺 anthropic-version")
		}
		if r.Header.Get("X-Api-Key") != "sk-a" {
			t.Error("探测请求缺 x-api-key")
		}
		w.Write([]byte(`{}`))
	}))
	defer up.Close()
	p := &config.Provider{Name: "a", Protocol: config.ProtocolAnthropic, BaseURL: up.URL, APIKey: "sk-a"}
	if _, err := ProbeProvider(p); err != nil {
		t.Fatal(err)
	}
}

// 任务1.4：.env 自动加载端到端（经 buildServer 注入，api_key_env 解析生效）。
func TestBuildServerLoadsEnvFile(t *testing.T) {
	var gotAuth atomic.Value
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth.Store(r.Header.Get("Authorization"))
		w.WriteHeader(200)
		io.WriteString(w, `{"ok":1}`)
	}))
	defer up.Close()
	root := writeRepo(t, map[string]string{
		"config/local.toml": `
[[providers]]
name = "relay"
protocol = "openai-chat"
base_url = "` + up.URL + `"
api_key_env = "AGW_DOTENV_E2E"
priority = 1
enabled = true

[gateway]
default_token = "agw-t"
`,
		".env": "AGW_DOTENV_E2E=sk-from-env-file\n",
	})
	s, cfg, err := buildServer(root)
	if err != nil {
		t.Fatal(err)
	}
	gw := httptest.NewServer(s.Handler())
	t.Cleanup(gw.Close)
	req, _ := http.NewRequest("POST", gw.URL+"/v1/messages", strings.NewReader(`{"model":"m","max_tokens":1}`))
	req.Header.Set("Authorization", "Bearer "+cfg.Gateway.DefaultToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if gotAuth.Load() != "Bearer sk-from-env-file" {
		t.Fatalf("上游认证 = %v，.env 未生效", gotAuth.Load())
	}
}

// 任务1.4：真实环境变量优先于 .env。
func TestBuildServerRealEnvWins(t *testing.T) {
	var gotAuth atomic.Value
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth.Store(r.Header.Get("Authorization"))
		w.WriteHeader(200)
		io.WriteString(w, `{"ok":1}`)
	}))
	defer up.Close()
	root := writeRepo(t, map[string]string{
		"config/local.toml": `
[[providers]]
name = "relay"
protocol = "openai-chat"
base_url = "` + up.URL + `"
api_key_env = "AGW_DOTENV_E2E2"
priority = 1
enabled = true

[gateway]
default_token = "agw-t"
`,
		".env": "AGW_DOTENV_E2E2=sk-from-env-file\n",
	})
	t.Setenv("AGW_DOTENV_E2E2", "sk-from-real-env")
	s, cfg, err := buildServer(root)
	if err != nil {
		t.Fatal(err)
	}
	gw := httptest.NewServer(s.Handler())
	t.Cleanup(gw.Close)
	req, _ := http.NewRequest("POST", gw.URL+"/v1/messages", strings.NewReader(`{"model":"m","max_tokens":1}`))
	req.Header.Set("Authorization", "Bearer "+cfg.Gateway.DefaultToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if gotAuth.Load() != "Bearer sk-from-real-env" {
		t.Fatalf("上游认证 = %v，真实环境变量应优先", gotAuth.Load())
	}
}

func TestResolveRootERejectsExplicitInvalidRoot(t *testing.T) {
	rootFlag = t.TempDir()
	t.Cleanup(func() { rootFlag = "" })

	if _, err := resolveRootE(); err == nil {
		t.Fatal("explicit --root without config/ should be rejected")
	}
}

func TestResolveRootExplicitFlagOverridesEnvironment(t *testing.T) {
	t.Setenv("AGW_ROOT", "/tmp/agw-definitely-invalid")
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	rootFlag = root
	t.Cleanup(func() { rootFlag = "" })

	if got, err := resolveRootE(); err != nil || got != root {
		t.Fatalf("resolveRootE() = %q,%v; want explicit root %q,nil", got, err, root)
	}
}

func TestPidIdentityFileAndProcessIdentity(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(runDir(root), 0o755)
	if _, json, err := readPidIdentity(root); json || err != nil {
		t.Fatalf("missing pidfile = json:%v err:%v", json, err)
	}
	if err := os.WriteFile(pidPath(root), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, json, err := readPidIdentity(root); json || err != nil {
		t.Fatalf("legacy pidfile = json:%v err:%v", json, err)
	}

	start, exe, ok := processIdentity(os.Getpid())
	if !ok || start <= 0 || exe == "" {
		t.Fatalf("current process identity unavailable: start=%d exe=%q ok=%v", start, exe, ok)
	}
	identity := pidIdentity{Pid: os.Getpid(), StartTime: start, Exe: exe, Root: root}
	if err := writePidIdentity(root, identity); err != nil {
		t.Fatal(err)
	}
	got, json, err := readPidIdentity(root)
	if err != nil || !json || got.Pid != identity.Pid || got.StartTime != identity.StartTime || got.Exe != identity.Exe || got.Root != identity.Root {
		t.Fatalf("identity roundtrip = %+v json:%v err:%v", got, json, err)
	}
	fi, err := os.Stat(pidPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("pidfile perm = %v", fi.Mode().Perm())
	}
	if !sameGatewayProcess(identity) {
		t.Fatal("current identity should match")
	}
	wrong := identity
	wrong.StartTime++
	if sameGatewayProcess(wrong) {
		t.Fatal("wrong start time must not match")
	}
	if _, _, ok := processIdentity(1 << 24); ok {
		t.Fatal("unlikely pid should not have readable identity")
	}
}

func TestStopGatewayRejectsMismatchedAndLegacyPidfiles(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(runDir(root), 0o755)
	start, _, ok := processIdentity(os.Getpid())
	if !ok {
		t.Fatal("cannot read current process identity")
	}
	mismatch := pidIdentity{Pid: os.Getpid(), StartTime: start + 1, Exe: "/definitely/not/agw", Root: root}
	if err := writePidIdentity(root, mismatch); err != nil {
		t.Fatal(err)
	}
	if err := StopGateway(root); err == nil || !strings.Contains(err.Error(), "身份不匹配") {
		t.Fatalf("mismatch error = %v", err)
	}
	if _, err := os.Stat(pidPath(root)); !os.IsNotExist(err) {
		t.Fatalf("mismatched pidfile should be removed: %v", err)
	}

	if err := os.WriteFile(pidPath(root), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := StopGateway(root); err == nil || !strings.Contains(err.Error(), "旧 pidfile") {
		t.Fatalf("legacy error = %v", err)
	}
	if _, err := os.Stat(pidPath(root)); !os.IsNotExist(err) {
		t.Fatalf("legacy pidfile should be removed: %v", err)
	}
}

func TestStartGatewayReadyTimeoutAndExitPaths(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(runDir(root), 0o755)
	script := filepath.Join(root, "alive.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	prevPath := selfPath
	prevReady := healthReady
	prevTimeout := startReadyTimeout
	startReadyTimeout = 150 * time.Millisecond
	selfPath = func() (string, error) { return script, nil }
	healthReady = func(string, string) bool { return false }
	t.Cleanup(func() {
		selfPath = prevPath
		healthReady = prevReady
		startReadyTimeout = prevTimeout
	})

	pid, err := StartGateway(root, "127.0.0.1:1")
	if err == nil || !strings.Contains(err.Error(), "healthz") {
		t.Fatalf("timeout error = %v", err)
	}
	if _, statErr := os.Stat(pidPath(root)); statErr == nil {
		t.Fatal("timeout must not leave pidfile")
	}
	proc, findErr := os.FindProcess(pid)
	if findErr != nil {
		t.Fatal(findErr)
	}
	if signalErr := proc.Signal(syscall.SIGKILL); signalErr != nil {
		t.Fatal(signalErr)
	}

	healthReady = func(string, string) bool { return true }
	readyPid, err := StartGateway(root, "127.0.0.1:1")
	if err != nil {
		t.Fatalf("ready path failed: %v", err)
	}
	identity, json, err := readPidIdentity(root)
	if err != nil || !json || identity.Pid != readyPid || identity.Root != root {
		t.Fatalf("ready pidfile identity = %+v json:%v err:%v", identity, json, err)
	}
	proc, _ = os.FindProcess(readyPid)
	if err := proc.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
}

func TestStartGatewayStillDetectsImmediateExit(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(runDir(root), 0o755)
	failScript := filepath.Join(root, "fail.sh")
	os.WriteFile(failScript, []byte("#!/bin/sh\necho 'listen tcp: bind: address already in use'\nexit 1\n"), 0o755)
	prevPath := selfPath
	prevReady := healthReady
	prevTimeout := startReadyTimeout
	startReadyTimeout = time.Second
	selfPath = func() (string, error) { return failScript, nil }
	healthReady = func(string, string) bool { return false }
	t.Cleanup(func() {
		selfPath = prevPath
		healthReady = prevReady
		startReadyTimeout = prevTimeout
	})

	_, err := StartGateway(root, "127.0.0.1:8787")
	if err == nil || !strings.Contains(err.Error(), "address already in use") {
		t.Fatalf("exit error = %v", err)
	}
	if _, statErr := os.Stat(pidPath(root)); statErr == nil {
		t.Fatal("failed start must not leave pidfile")
	}
}
