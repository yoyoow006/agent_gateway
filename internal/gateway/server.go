// Package gateway 实现本地路由网关：端点、令牌档案、failover 与协议翻译。
package gateway

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"agent_gateway/internal/config"
	"agent_gateway/internal/protocol"
	"agent_gateway/internal/protocol/anthropic"
	"agent_gateway/internal/protocol/openaichat"
	"agent_gateway/internal/protocol/openairesponses"
	"agent_gateway/internal/provider"
)

// Server 是网关实例；配置可热重载。
type Server struct {
	mu       sync.RWMutex
	cfg      *config.Config
	registry *provider.Registry
	clients  map[string]*http.Client
	reload   func() (*config.Config, error)
	logger   *log.Logger
	started  time.Time
}

// New 构造网关；reload 为 nil 时 /__agw/reload 返回未配置。
func New(cfg *config.Config, reload func() (*config.Config, error), logger *log.Logger) *Server {
	if logger == nil {
		logger = log.New(io.Discard, "", log.LstdFlags)
	}
	s := &Server{
		cfg:      cfg,
		registry: provider.NewRegistry(),
		clients:  map[string]*http.Client{},
		reload:   reload,
		logger:   logger,
		started:  time.Now(),
	}
	s.syncBreakers()
	return s
}

// syncBreakers 同步注册表与当前配置的供应商集合（保留既有熔断状态）。
func (s *Server) syncBreakers() {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := map[string]bool{}
	for _, p := range s.cfg.Providers {
		names[p.Name] = true
		s.registry.Upsert(p.Name, provider.DefaultConfig(), nil)
	}
	for _, snap := range s.registry.Snapshot() {
		if !names[snap.Name] {
			s.registry.Remove(snap.Name)
		}
	}
}

// Reload 用加载函数的结果热重载配置；失败保留旧配置。
func (s *Server) Reload() error {
	if s.reload == nil {
		return nil
	}
	newCfg, err := s.reload()
	if err != nil {
		s.logger.Printf("[agw] 配置重载失败，保留旧配置: %v", err)
		return err
	}
	s.mu.Lock()
	s.cfg = newCfg
	s.mu.Unlock()
	s.syncBreakers()
	return nil
}

// Config 返回当前配置快照。
func (s *Server) Config() *config.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// codecFor 返回协议编解码器。
func codecFor(p config.Protocol) protocol.Codec {
	switch p {
	case config.ProtocolAnthropic:
		return anthropic.DefaultCodec
	case config.ProtocolOpenAIChat:
		return openaichat.DefaultCodec
	case config.ProtocolOpenAIResponses:
		return openairesponses.DefaultCodec
	}
	return nil
}

// Handler 返回根路由。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages/count_tokens", s.withAuth(config.ProtocolAnthropic, s.handleCountTokens))
	mux.HandleFunc("/v1/messages", s.withAuth(config.ProtocolAnthropic, s.handleForward(config.ProtocolAnthropic)))
	mux.HandleFunc("/v1/responses", s.withAuth(config.ProtocolOpenAIResponses, s.handleForward(config.ProtocolOpenAIResponses)))
	mux.HandleFunc("/v1/chat/completions", s.withAuth(config.ProtocolOpenAIChat, s.handleForward(config.ProtocolOpenAIChat)))
	mux.HandleFunc("/v1/models", s.withAuth(config.ProtocolOpenAIResponses, s.handleModels))
	mux.HandleFunc("/__agw/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		io.WriteString(w, "ok")
	})
	mux.HandleFunc("/__agw/metrics", s.admin(s.handleMetrics))
	mux.HandleFunc("/__agw/reload", s.admin(s.handleReload))
	return mux
}

// clientToken 从请求提取虚拟令牌。
func clientToken(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	if k := r.Header.Get("X-Api-Key"); k != "" {
		return k
	}
	return ""
}

type ctxKey string

const profileKey ctxKey = "agw.profile"

// withAuth 解析令牌→项目档案，失败按客户端协议返回 401。
func (s *Server) withAuth(clientProto config.Protocol, next func(http.ResponseWriter, *http.Request, *config.Profile)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := clientToken(r)
		cfg := s.Config()
		var profile *config.Profile
		if token != "" {
			if project, ok := cfg.TokenProject(token); ok {
				profile, _ = cfg.ResolveProfile(project)
			}
		}
		if profile == nil {
			writeError(w, codecFor(clientProto), 401, "未知或缺失虚拟令牌（agw run / agw install 会自动注入）")
			return
		}
		next(w, r, profile)
	}
}

// admin 要求 admin 令牌。
func (s *Server) admin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if clientToken(r) != s.Config().AdminToken {
			w.WriteHeader(401)
			io.WriteString(w, `{"error":"admin token required"}`)
			return
		}
		next(w, r)
	}
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	cfg := s.Config()
	out := map[string]any{
		"listen":     cfg.Gateway.Listen,
		"uptime_sec": int(time.Since(s.started).Seconds()),
		"providers":  s.registry.Snapshot(),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

func (s *Server) handleReload(w http.ResponseWriter, r *http.Request) {
	if err := s.Reload(); err != nil {
		w.WriteHeader(500)
		io.WriteString(w, `{"error":"`+err.Error()+`"}`)
		return
	}
	w.WriteHeader(200)
	io.WriteString(w, `{"ok":true}`)
}

// handleModels 返回配置内模型列表（OpenAI 格式，兼容两种客户端）。
func (s *Server) handleModels(w http.ResponseWriter, r *http.Request, profile *config.Profile) {
	models := map[string]bool{}
	for _, p := range profile.Chain {
		for from := range p.ModelMap {
			models[from] = true
		}
	}
	if len(models) == 0 {
		for _, p := range profile.Chain {
			models["via-"+p.Name] = true
		}
	}
	data := make([]map[string]any, 0, len(models))
	for m := range models {
		data = append(data, map[string]any{"id": m, "object": "model", "owned_by": "agw"})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": data})
}

// writeError 按客户端协议写错误体。
func writeError(w http.ResponseWriter, codec protocol.Codec, status int, msg string) {
	body := codec.BuildError(status, msg)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(body)
}
