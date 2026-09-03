package cli

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"agent_gateway/internal/config"
	"agent_gateway/internal/gateway"
	"agent_gateway/internal/protocol"

	"github.com/spf13/cobra"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "前台运行网关（调试用；常驻请用 agw start）",
	Run:   runServe,
}

func init() {
	addRootFlag(serveCmd)
	rootCommands = append(rootCommands, serveCmd)
}

// buildServer 组装网关实例（含密钥补齐、降级告警钩子、热重载函数）。
func buildServer(root string) (*gateway.Server, *config.Config, error) {
	cfg, err := config.Load(root)
	if err != nil {
		return nil, nil, err
	}
	if err := config.EnsureSecrets(root, cfg); err != nil {
		return nil, nil, fmt.Errorf("生成令牌失败: %w", err)
	}
	logger := log.New(&redactWriter{w: os.Stdout}, "", log.LstdFlags)
	protocol.DropHook = func(detail string) {
		logger.Printf("[agw][翻译降级] %s", detail)
	}
	reloadFn := func() (*config.Config, error) { return config.Load(root) }
	return gateway.New(cfg, reloadFn, logger), cfg, nil
}

// redactWriter 把写出的日志行做密钥脱敏。
type redactWriter struct {
	w io.Writer
}

func (rw *redactWriter) Write(p []byte) (int, error) {
	n, err := rw.w.Write([]byte(Redact(string(p))))
	return n, err
}

func runServe(cmd *cobra.Command, args []string) {
	root := resolveRoot()
	s, cfg, err := buildServer(root)
	if err != nil {
		fatalf("%v", err)
	}
	listen := cfg.Gateway.Listen
	if host, _, err := net.SplitHostPort(listen); err == nil {
		if host != "127.0.0.1" && host != "localhost" && host != "::1" && host != "" {
			fmt.Fprintf(os.Stderr, "⚠️  监听地址 %s 不是回环地址，网关将暴露给局域网，请确认\n", listen)
		}
	}
	fmt.Printf("agw 网关监听 %s（项目根 %s）\n", listen, root)

	httpSrv := &http.Server{Addr: listen, Handler: s.Handler()}

	// SIGHUP 热重载
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	go func() {
		for range hup {
			if err := s.Reload(); err != nil {
				fmt.Fprintf(os.Stderr, "重载失败，继续使用旧配置: %v\n", err)
			} else {
				fmt.Println("配置已热重载")
			}
		}
	}()

	// 优雅停机
	term := make(chan os.Signal, 1)
	signal.Notify(term, syscall.SIGINT, syscall.SIGTERM)
	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.ListenAndServe() }()

	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			fatalf("监听失败: %v", err)
		}
	case sig := <-term:
		fmt.Printf("收到 %v，排空在途请求（上限 15s）…\n", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(ctx)
	}
}
