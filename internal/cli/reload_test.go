package cli

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

// 任务 1.3：网关未运行 → agw reload 幂等退出 0，且不向任何端点发请求。
func TestRunReloadGatewayNotRunning(t *testing.T) {
	root := writeRepo(t, map[string]string{
		"config/default.toml": "",
	})
	// 不写 pidfile → 网关视为未运行
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	cmd := NewRootCmd()
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"reload", "--root", root})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("未运行场景应成功（退出 0）: %v", err)
	}
	if hits.Load() != 0 {
		t.Errorf("未运行不应触发任何 HTTP，hits=%d", hits.Load())
	}
}

// 任务 1.3：网关运行 + reload 端点返回 200 → agw reload 退出 0，恰好 1 次 POST /__agw/reload。
func TestRunReloadSuccess(t *testing.T) {
	root := writeRepo(t, map[string]string{
		"config/default.toml": "",
	})
	var posts atomic.Int64
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/__agw/reload" && r.Method == http.MethodPost {
			posts.Add(1)
			if r.Header.Get("Authorization") != "Bearer agw-test" {
				w.WriteHeader(401)
				return
			}
			w.Write([]byte(`{"ok":true}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer fake.Close()

	// admin_token 是顶层字段（不在 [gateway] 下）；listen 通过 local.toml 覆盖指向 fake。
	cfgRaw := []byte(`
admin_token = "agw-test"

[gateway]
listen = "` + strings.TrimPrefix(fake.URL, "http://") + `"
`)
	os.MkdirAll(filepath.Join(root, "config"), 0o755)
	os.WriteFile(filepath.Join(root, "config", "local.toml"), cfgRaw, 0o600)

	// 写 pidfile 为当前进程 → pidAlive 探测成功
	os.MkdirAll(runDir(root), 0o755)
	os.WriteFile(pidPath(root), []byte(strconv.Itoa(os.Getpid())), 0o600)

	cmd := NewRootCmd()
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"reload", "--root", root})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("reload 200 应退出 0: %v", err)
	}
	if posts.Load() != 1 {
		t.Errorf("应触发 1 次 reload POST，got %d", posts.Load())
	}
}

// 任务 1.3：网关运行 + reload 端点返回 500 → agw reload 退出非零（2）。
func TestRunReloadEndpointError(t *testing.T) {
	root := writeRepo(t, map[string]string{
		"config/default.toml": "",
	})
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/__agw/reload" {
			w.WriteHeader(500)
			_, _ = w.Write([]byte(`{"error":"bad config"}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer fake.Close()

	cfgRaw := []byte(`
admin_token = "agw-test"

[gateway]
listen = "` + strings.TrimPrefix(fake.URL, "http://") + `"
`)
	os.MkdirAll(filepath.Join(root, "config"), 0o755)
	os.WriteFile(filepath.Join(root, "config", "local.toml"), cfgRaw, 0o600)
	os.MkdirAll(runDir(root), 0o755)
	os.WriteFile(pidPath(root), []byte(strconv.Itoa(os.Getpid())), 0o600)

	cmd := NewRootCmd()
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"reload", "--root", root})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("reload 500 应返回非零退出")
	}
	if !errors.Is(err, ErrReloadFailed) {
		t.Fatalf("reload 500 应映射为 ErrReloadFailed（main.go 据此退出码 2），got %v", err)
	}
}

// 任务 1.3：local.toml 解析失败 → agw reload 退出 1，不发 HTTP 请求。
func TestRunReloadParseError(t *testing.T) {
	root := writeRepo(t, map[string]string{
		"config/default.toml": "",
	})
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	os.MkdirAll(filepath.Join(root, "config"), 0o755)
	os.WriteFile(filepath.Join(root, "config", "local.toml"), []byte("broken [toml\n"), 0o600)

	cmd := NewRootCmd()
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"reload", "--root", root})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("TOML 解析失败应退出非零")
	}
	if hits.Load() != 0 {
		t.Errorf("解析失败不应发 HTTP，hits=%d", hits.Load())
	}
}

func TestResolveRootERejectsInvalidAGWRoot(t *testing.T) {
	t.Setenv("AGW_ROOT", t.TempDir())
	rootFlag = ""
	t.Cleanup(func() { rootFlag = "" })

	if _, err := resolveRootE(); err == nil {
		t.Fatal("AGW_ROOT without config/ should be rejected")
	}
}
