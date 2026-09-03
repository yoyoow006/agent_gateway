// Package agent 适配 Claude Code 与 Codex：一键安装与项目上下文启动。
package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

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
	Listen string // 网关监听地址
	Token  string // 全局虚拟令牌
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

// backup 复制文件为 <名>.agw-backup-<时间戳>，返回备份路径。
func backup(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	dst := fmt.Sprintf("%s.agw-backup-%s", path, time.Now().Format("20060102-150405"))
	return dst, os.WriteFile(dst, data, 0o600)
}

// npmInstall 用 npm -g 安装包。
func npmInstall(r Runner, pkg string) error {
	if err := r.Run("npm", "--version"); err != nil {
		return fmt.Errorf("未检测到 npm（安装 %s 需要 Node.js/npm）：请先安装 Node.js ≥18，见 https://nodejs.org", pkg)
	}
	return r.Run("npm", "install", "-g", pkg)
}

// InstallClaude 安装 Claude Code 并把 settings.json 指向网关。
// 安全合并：先备份，仅写 env.ANTHROPIC_BASE_URL / env.ANTHROPIC_AUTH_TOKEN，其余用户键原样保留。
func InstallClaude(opts Options) error {
	opts.fill()
	if err := npmInstall(opts.Runner, "@anthropic-ai/claude-code"); err != nil {
		return err
	}
	claudeDir := filepath.Join(opts.Home, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return err
	}
	settingsPath := filepath.Join(claudeDir, "settings.json")
	settings := map[string]any{}
	if data, err := os.ReadFile(settingsPath); err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("settings.json 已损坏，拒绝写入（请先修复或删除）: %w", err)
		}
		if _, err := backup(settingsPath); err != nil {
			return fmt.Errorf("备份失败，已中止写入: %w", err)
		}
	}
	env, _ := settings["env"].(map[string]any)
	if env == nil {
		env = map[string]any{}
	}
	env["ANTHROPIC_BASE_URL"] = "http://" + opts.Listen
	env["ANTHROPIC_AUTH_TOKEN"] = opts.Token
	settings["env"] = env
	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(settingsPath, out, 0o600); err != nil {
		return err
	}
	fmt.Printf("Claude Code 已安装并指向网关 %s\n回滚：删除 settings.json 中 env 的两个 ANTHROPIC_ 键，或用同目录 .agw-backup-* 恢复\n", opts.Listen)
	return nil
}

// InstallCodex 安装 Codex 并把 config.toml 指向网关。
// 合并 [model_providers.agw]（wire_api=responses、env_key=AGW_API_KEY）、
// model_provider="agw"、顶层 disable_response_storage=true（无状态可切换前提）。
// 注意：TOML 重写会丢注释，已先备份。
func InstallCodex(opts Options) error {
	opts.fill()
	if err := npmInstall(opts.Runner, "@openai/codex"); err != nil {
		return err
	}
	codexDir := filepath.Join(opts.Home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		return err
	}
	cfgPath := filepath.Join(codexDir, "config.toml")
	cfg := map[string]any{}
	if data, err := os.ReadFile(cfgPath); err == nil {
		if _, err := toml.Decode(string(data), &cfg); err != nil {
			return fmt.Errorf("config.toml 已损坏，拒绝写入（请先修复）: %w", err)
		}
		if _, err := backup(cfgPath); err != nil {
			return fmt.Errorf("备份失败，已中止写入: %w", err)
		}
	}
	cfg["model_provider"] = "agw"
	cfg["disable_response_storage"] = true
	provs, _ := cfg["model_providers"].(map[string]any)
	if provs == nil {
		provs = map[string]any{}
	}
	provs["agw"] = map[string]any{
		"name":     "agw",
		"base_url": "http://" + opts.Listen + "/v1",
		"env_key":  "AGW_API_KEY",
		"wire_api": "responses",
	}
	cfg["model_providers"] = provs
	var sb strings.Builder
	if err := toml.NewEncoder(&sb).Encode(cfg); err != nil {
		return err
	}
	if err := os.WriteFile(cfgPath, []byte(sb.String()), 0o600); err != nil {
		return err
	}
	fmt.Printf("Codex 已安装并指向网关 %s/v1（wire_api=responses；已写入响应存储禁用键——认得该键的旧版本会生效，新版 codex 默认即无状态）\n", opts.Listen)
	fmt.Printf("使用前设置环境变量 AGW_API_KEY=<agw 全局令牌>（agw run codex 会自动注入）\n")
	fmt.Printf("回滚：用同目录 .agw-backup-* 恢复 config.toml（重写可能丢失注释）\n")
	return nil
}
