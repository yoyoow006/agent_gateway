// Package agent 适配 Claude Code 与 Codex：一键安装与项目上下文启动。
// 配置策略：零接触用户默认配置——claude 用 `--settings <独立文件>`，
// codex 用 `-p agw`（`$CODEX_HOME/agw.config.toml` 独立 profile 文件）。
package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Runner 抽象外部命令执行（测试注入）。
type Runner interface {
	Run(name string, args ...string) error
}

type execRunner struct{}

func (execRunner) Run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Options 是安装参数。
type Options struct {
	Home   string // 用户主目录（默认 os.UserHomeDir）
	Root   string // 网关仓库根（.agw/ 独立配置所在）
	Listen string // 网关监听地址
	Runner Runner // 默认真实执行
}

func (o *Options) fill() {
	if o.Home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			o.Home = h
		}
	}
	if o.Runner == nil {
		o.Runner = execRunner{}
	}
}

// CodexHome 返回 codex 配置目录（$CODEX_HOME 优先，缺省 ~/.codex）。
func CodexHome(home string) string {
	if v := os.Getenv("CODEX_HOME"); v != "" {
		return v
	}
	return filepath.Join(home, ".codex")
}

// npmInstall 用 npm -g 安装包。
func npmInstall(r Runner, pkg string) error {
	if err := r.Run("npm", "--version"); err != nil {
		return fmt.Errorf("未检测到 npm（安装 %s 需要 Node.js/npm）：请先安装 Node.js ≥18，见 https://nodejs.org", pkg)
	}
	return r.Run("npm", "install", "-g", pkg)
}

// GenerateClaudeSettings 生成（或重写）`.agw/claude-settings.<项目|global>.json`，
// 内容为指向网关与项目令牌的 env 两键；返回文件路径。0600（含令牌）。
func GenerateClaudeSettings(root, project, listen, token string) (string, error) {
	name := project
	if name == "" {
		name = "global"
	}
	dir := filepath.Join(root, ".agw")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	settings := map[string]any{
		"env": map[string]any{
			"ANTHROPIC_BASE_URL":   "http://" + listen,
			"ANTHROPIC_AUTH_TOKEN": token,
		},
	}
	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("claude-settings.%s.json", name))
	if err := os.WriteFile(path, out, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// EnsureCodexProfile 幂等写入 `$CODEX_HOME/agw.config.toml`（独立 profile 文件，
// 由 `codex -p agw` 叠加在用户 config.toml 之上）。不含密钥（env_key 引用环境变量）。
func EnsureCodexProfile(codexHome, listen string) error {
	if err := os.MkdirAll(codexHome, 0o755); err != nil {
		return err
	}
	cfg := map[string]any{
		"model_provider":           "agw",
		"disable_response_storage": true,
		"model_providers": map[string]any{
			"agw": map[string]any{
				"name":     "agw",
				"base_url": "http://" + listen + "/v1",
				"env_key":  "AGW_API_KEY",
				"wire_api": "responses",
			},
		},
	}
	var sb strings.Builder
	if err := toml.NewEncoder(&sb).Encode(cfg); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(codexHome, "agw.config.toml"), []byte(sb.String()), 0o600)
}

// InstallClaude 安装 Claude Code 并预生成独立配置目录；不触碰 ~/.claude。
func InstallClaude(opts Options) error {
	opts.fill()
	if err := npmInstall(opts.Runner, "@anthropic-ai/claude-code"); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(opts.Root, ".agw"), 0o755); err != nil {
		return err
	}
	fmt.Printf("Claude Code 已安装。\n使用：agw run claude [--project <名>]（经 --settings 独立配置启动，你的 ~/.claude/settings.json 不会被修改）\n")
	return nil
}

// InstallCodex 安装 Codex 并生成独立 profile；不触碰 config.toml。
func InstallCodex(opts Options) error {
	opts.fill()
	if err := npmInstall(opts.Runner, "@openai/codex"); err != nil {
		return err
	}
	if err := EnsureCodexProfile(CodexHome(opts.Home), opts.Listen); err != nil {
		return err
	}
	fmt.Printf("Codex 已安装，独立 profile：%s\n", filepath.Join(CodexHome(opts.Home), "agw.config.toml"))
	fmt.Printf("使用：agw run codex [--project <名>]（经 -p agw 启动并注入 AGW_API_KEY，你的 config.toml 不会被修改）\n")
	fmt.Printf("回滚：删除上述 agw.config.toml 即可（用户配置从未被改动）\n")
	return nil
}
