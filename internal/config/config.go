// Package config 负责 agw 的配置加载、三层合并与本地密钥管理。
package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Protocol 是供应商说的线上协议。
type Protocol string

const (
	ProtocolAnthropic       Protocol = "anthropic"
	ProtocolOpenAIChat      Protocol = "openai-chat"
	ProtocolOpenAIResponses Protocol = "openai-responses"
)

// Valid 报告协议是否合法。
func (p Protocol) Valid() bool {
	switch p {
	case ProtocolAnthropic, ProtocolOpenAIChat, ProtocolOpenAIResponses:
		return true
	}
	return false
}

// Provider 是一个上游供应商条目。
type Provider struct {
	Name              string            `toml:"name"`
	Protocol          Protocol          `toml:"protocol"`
	BaseURL           string            `toml:"base_url"`
	APIKey            string            `toml:"api_key,omitempty"`
	APIKeyEnv         string            `toml:"api_key_env,omitempty"`
	Priority          int               `toml:"priority"`
	Enabled           bool              `toml:"enabled"`
	Preferred         bool              `toml:"preferred,omitempty"`
	ModelMap          map[string]string `toml:"model_map,omitempty"`
	Headers           map[string]string `toml:"headers,omitempty"`
	ConnectTimeoutSec int               `toml:"connect_timeout_sec,omitempty"`
	FirstByteTimeout  int               `toml:"first_byte_timeout_sec,omitempty"`
}

// ResolveAPIKey 返回密钥：字面量优先于 env:VAR 间接；都没有则报错。
func (p *Provider) ResolveAPIKey() (string, error) {
	if p.APIKey != "" {
		return p.APIKey, nil
	}
	if p.APIKeyEnv != "" {
		v := os.Getenv(p.APIKeyEnv)
		if v == "" {
			return "", fmt.Errorf("供应商 %s 的环境变量 %s 未设置", p.Name, p.APIKeyEnv)
		}
		return v, nil
	}
	return "", fmt.Errorf("供应商 %s 未配置 api_key 或 api_key_env", p.Name)
}

// GatewayCfg 是网关本身的配置。
type GatewayCfg struct {
	Listen       string `toml:"listen"`
	DefaultToken string `toml:"default_token,omitempty"`
	LogLevel     string `toml:"log_level,omitempty"`
}

// ProjectProfile 是项目档案：覆盖全局池的子集与偏好；令牌只存 local 层。
type ProjectProfile struct {
	Providers []string          `toml:"providers,omitempty"`
	Preferred string            `toml:"preferred,omitempty"`
	ModelMap  map[string]string `toml:"model_map,omitempty"`
	Token     string            `toml:"token,omitempty"`
}

// Config 是三层合并后的完整配置视图。
type Config struct {
	RepoRoot string `toml:"-"`

	Gateway    GatewayCfg                `toml:"gateway"`
	AdminToken string                    `toml:"admin_token,omitempty"`
	Providers  []Provider                `toml:"providers"`
	Projects   map[string]ProjectProfile `toml:"projects,omitempty"`

	tokenIndex map[string]string `toml:"-"`
}

// Profile 是一次请求实际使用的解析结果。
type Profile struct {
	Name      string
	Chain     []*Provider
	Preferred string
	ModelMap  map[string]string
	Token     string
}

// DefaultListen 是默认监听地址（仅回环）。
const DefaultListen = "127.0.0.1:8787"

// DefaultMaxBodyBytes 是 failover 重放缓冲上限（64MiB）。
const DefaultMaxBodyBytes = 64 << 20

// Load 读取并合并 default.toml ← local.toml ← projects/*/agw.toml。
// default/local 缺失不报错（至少要有一个供应商才能实际服务）。
func Load(root string) (*Config, error) {
	cfg := &Config{
		RepoRoot: root,
		Gateway:  GatewayCfg{Listen: DefaultListen},
		Projects: map[string]ProjectProfile{},
	}

	// 第一层：default.toml（可选）。
	if err := loadFile(filepath.Join(root, "config", "default.toml"), cfg); err != nil {
		return nil, err
	}
	// 第二层：local.toml（可选，含密钥）。
	if err := loadFile(filepath.Join(root, "config", "local.toml"), cfg); err != nil {
		return nil, err
	}
	// 第三层：projects/*/agw.toml（可选，项目覆盖，令牌除外）。
	projDir := filepath.Join(root, "projects")
	entries, err := os.ReadDir(projDir)
	if err == nil {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if e.IsDir() {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		for _, name := range names {
			var proj struct {
				Project ProjectProfile `toml:"project"`
			}
			path := filepath.Join(projDir, name, "agw.toml")
			if _, err := os.Stat(path); err != nil {
				continue
			}
			if _, err := toml.DecodeFile(path, &proj); err != nil {
				return nil, fmt.Errorf("解析 %s: %w", path, err)
			}
			p := cfg.Projects[name]
			overlayProject(&p, proj.Project)
			cfg.Projects[name] = p
		}
	}

	cfg.rebuildTokenIndex()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// loadFile 把一个 TOML 文件按"后读覆盖"合并进 cfg。
// providers 按名字做字段级合并；gateway/projects 做字段覆盖。
func loadFile(path string, cfg *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var raw struct {
		Gateway    GatewayCfg                `toml:"gateway"`
		AdminToken string                    `toml:"admin_token"`
		Providers  []map[string]any          `toml:"providers"`
		Projects   map[string]ProjectProfile `toml:"projects"`
	}
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return fmt.Errorf("解析 %s: %w", path, err)
	}
	if raw.Gateway.Listen != "" {
		cfg.Gateway.Listen = raw.Gateway.Listen
	}
	if raw.Gateway.DefaultToken != "" {
		cfg.Gateway.DefaultToken = raw.Gateway.DefaultToken
	}
	if raw.Gateway.LogLevel != "" {
		cfg.Gateway.LogLevel = raw.Gateway.LogLevel
	}
	if raw.AdminToken != "" {
		cfg.AdminToken = raw.AdminToken
	}
	for _, pm := range raw.Providers {
		mergeProviderMap(cfg, pm)
	}
	for name, p := range raw.Projects {
		existing := cfg.Projects[name]
		overlayProject(&existing, p)
		cfg.Projects[name] = existing
	}
	return nil
}

// mergeProviderMap 把一个 provider TOML 片段按名字合并进 cfg.Providers。
func mergeProviderMap(cfg *Config, pm map[string]any) {
	name, _ := pm["name"].(string)
	idx := -1
	for i := range cfg.Providers {
		if cfg.Providers[i].Name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		p := Provider{Name: name}
		applyProviderFields(&p, pm)
		cfg.Providers = append(cfg.Providers, p)
		return
	}
	applyProviderFields(&cfg.Providers[idx], pm)
}

// applyProviderFields 只覆盖片段中出现的字段（name 由调用方负责）。
func applyProviderFields(p *Provider, pm map[string]any) {
	if v, ok := pm["protocol"].(string); ok {
		p.Protocol = Protocol(v)
	}
	if v, ok := pm["base_url"].(string); ok {
		p.BaseURL = v
	}
	if v, ok := pm["api_key"].(string); ok {
		p.APIKey = v
	}
	if v, ok := pm["api_key_env"].(string); ok {
		p.APIKeyEnv = v
	}
	if v, ok := pm["priority"].(int64); ok {
		p.Priority = int(v)
	}
	if v, ok := pm["enabled"].(bool); ok {
		p.Enabled = v
	}
	if v, ok := pm["preferred"].(bool); ok {
		p.Preferred = v
	}
	if v, ok := pm["connect_timeout_sec"].(int64); ok {
		p.ConnectTimeoutSec = int(v)
	}
	if v, ok := pm["first_byte_timeout_sec"].(int64); ok {
		p.FirstByteTimeout = int(v)
	}
	if v, ok := pm["model_map"].(map[string]any); ok {
		if p.ModelMap == nil {
			p.ModelMap = map[string]string{}
		}
		for k, mv := range v {
			if s, ok := mv.(string); ok {
				p.ModelMap[k] = s
			}
		}
	}
	if v, ok := pm["headers"].(map[string]any); ok {
		if p.Headers == nil {
			p.Headers = map[string]string{}
		}
		for k, mv := range v {
			if s, ok := mv.(string); ok {
				p.Headers[k] = s
			}
		}
	}
}

// overlayProject 用 src 中非零字段覆盖 dst。
func overlayProject(dst *ProjectProfile, src ProjectProfile) {
	if len(src.Providers) > 0 {
		dst.Providers = src.Providers
	}
	if src.Preferred != "" {
		dst.Preferred = src.Preferred
	}
	if len(src.ModelMap) > 0 {
		if dst.ModelMap == nil {
			dst.ModelMap = map[string]string{}
		}
		for k, v := range src.ModelMap {
			dst.ModelMap[k] = v
		}
	}
	if src.Token != "" {
		dst.Token = src.Token
	}
}

func (c *Config) validate() error {
	seen := map[string]bool{}
	for i, p := range c.Providers {
		if p.Name == "" {
			return fmt.Errorf("第 %d 个供应商缺少 name", i+1)
		}
		if seen[p.Name] {
			return fmt.Errorf("供应商 %s 重复定义", p.Name)
		}
		seen[p.Name] = true
		if !p.Enabled {
			continue
		}
		if !p.Protocol.Valid() {
			return fmt.Errorf("供应商 %s 协议非法: %q", p.Name, p.Protocol)
		}
		if p.BaseURL == "" {
			return fmt.Errorf("供应商 %s 缺少 base_url", p.Name)
		}
	}
	return nil
}

// rebuildTokenIndex 重建 令牌→项目 索引（"" 表示全局默认档案）。
func (c *Config) rebuildTokenIndex() {
	c.tokenIndex = map[string]string{}
	if c.Gateway.DefaultToken != "" {
		c.tokenIndex[c.Gateway.DefaultToken] = ""
	}
	for name, p := range c.Projects {
		if p.Token != "" {
			c.tokenIndex[p.Token] = name
		}
	}
}

// TokenProject 反查虚拟令牌所属项目。
func (c *Config) TokenProject(token string) (project string, ok bool) {
	project, ok = c.tokenIndex[token]
	return
}

// Provider 按名字查找供应商。
func (c *Config) Provider(name string) *Provider {
	for i := range c.Providers {
		if c.Providers[i].Name == name {
			return &c.Providers[i]
		}
	}
	return nil
}

// ResolveProfile 解析项目（"" 为全局）实际使用的供应商链与模型映射。
func (c *Config) ResolveProfile(project string) (*Profile, error) {
	prof := Profile{Name: project}
	var chainNames []string
	if p, ok := c.Projects[project]; ok {
		chainNames = p.Providers
		prof.Preferred = p.Preferred
		prof.ModelMap = p.ModelMap
		prof.Token = p.Token
	}
	if prof.Token == "" && project == "" {
		prof.Token = c.Gateway.DefaultToken
	}

	var chain []*Provider
	if len(chainNames) > 0 {
		for _, name := range chainNames {
			p := c.Provider(name)
			if p == nil {
				return nil, fmt.Errorf("项目 %s 引用了不存在的供应商 %s", project, name)
			}
			if !p.Enabled {
				continue
			}
			chain = append(chain, p)
		}
	} else {
		for i := range c.Providers {
			if c.Providers[i].Enabled {
				chain = append(chain, &c.Providers[i])
			}
		}
		sort.Slice(chain, func(i, j int) bool {
			if chain[i].Priority != chain[j].Priority {
				return chain[i].Priority < chain[j].Priority
			}
			return chain[i].Name < chain[j].Name
		})
	}

	// 粘性首选置顶：档案 preferred 优先，其次 provider 级开关。
	preferred := prof.Preferred
	if preferred == "" {
		for _, p := range chain {
			if p.Preferred {
				preferred = p.Name
				break
			}
		}
	}
	prof.Preferred = preferred
	if preferred != "" {
		sort.SliceStable(chain, func(i, j int) bool {
			return chain[i].Name == preferred && chain[j].Name != preferred
		})
	}
	prof.Chain = chain
	return &prof, nil
}

// NewToken 生成 agw-<64hex> 虚拟令牌。
func NewToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return "agw-" + hex.EncodeToString(b)
}

// EnsureSecrets 确保 admin/default 令牌存在，缺失则生成并写入 local.toml。
func EnsureSecrets(root string, cfg *Config) error {
	changed := false
	if cfg.AdminToken == "" {
		cfg.AdminToken = NewToken()
		changed = true
	}
	if cfg.Gateway.DefaultToken == "" {
		cfg.Gateway.DefaultToken = NewToken()
		changed = true
	}
	if !changed {
		return nil
	}
	return SaveLocal(root, cfg)
}

// SaveLocal 把当前配置写回 config/local.toml（0600）。
// 写入完整状态；与 default.toml 重复的条目字段级合并后语义不变。
func SaveLocal(root string, cfg *Config) error {
	dir := filepath.Join(root, "config")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	out := struct {
		Gateway    GatewayCfg                `toml:"gateway"`
		AdminToken string                    `toml:"admin_token,omitempty"`
		Providers  []Provider                `toml:"providers"`
		Projects   map[string]ProjectProfile `toml:"projects,omitempty"`
	}{Gateway: cfg.Gateway, AdminToken: cfg.AdminToken, Providers: cfg.Providers, Projects: cfg.Projects}
	var sb strings.Builder
	if err := toml.NewEncoder(&sb).Encode(out); err != nil {
		return err
	}
	path := filepath.Join(dir, "local.toml")
	if err := os.WriteFile(path, []byte(sb.String()), 0o600); err != nil {
		return err
	}
	// 已存在文件保留原权限位之上的收紧；WriteFile 不裁剪已存在文件权限。
	if fi, err := os.Stat(path); err == nil && fi.Mode().Perm() != 0o600 {
		_ = os.Chmod(path, 0o600)
	}
	cfg.rebuildTokenIndex()
	return nil
}

// FindRoot 从 start（含）向上寻找网关仓库根（含 config/default.toml 或 go.mod）。
// AGW_ROOT 环境变量优先。
func FindRoot(start string) (string, error) {
	if v := os.Getenv("AGW_ROOT"); v != "" {
		if _, err := os.Stat(filepath.Join(v, "config")); err == nil {
			return v, nil
		}
		return "", fmt.Errorf("AGW_ROOT=%s 不是有效网关仓库", v)
	}
	dir := start
	for {
		for _, marker := range []string{filepath.Join("config", "default.toml"), "go.mod"} {
			if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
				return dir, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("未找到网关仓库根（向上未发现 config/default.toml 或 go.mod）；请用 --root 指定")
		}
		dir = parent
	}
}
