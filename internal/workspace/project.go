// Package workspace 管理 projects/ 业务项目工作区。
package workspace

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"agent_gateway/internal/config"
)

// validNamePattern 是项目名规则。
const validNamePattern = `^[a-z0-9][a-z0-9_-]*$`

// Runner 抽象命令执行（测试注入）。
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

// agwTomlTemplate 是项目覆盖配置模板。
const agwTomlTemplate = `# 项目路由覆盖（提交到业务仓库；令牌等密钥只存网关 config/local.toml）
# providers：按序启用的供应商子集（留空 = 继承全局池按优先级）
# preferred：粘性首选（健康时优先，减少 prompt cache 失效）
# model_map：请求模型 → 实际模型（叠加在供应商映射之上）

[project]
# providers = ["relay1", "official-anthropic"]
# preferred = "relay1"

# [project.model_map]
# "claude-sonnet-5" = "claude-sonnet-5-relay"
`

// New 创建项目：目录 + git init + agw.toml 模板 + 虚拟令牌。
// git 缺失时告警跳过。返回生成的项目令牌。
func New(root, name string, runner Runner) (token string, err error) {
	if !regexp.MustCompile(validNamePattern).MatchString(name) {
		return "", fmt.Errorf("项目名非法: %q（规则 %s）", name, validNamePattern)
	}
	dir := filepath.Join(root, "projects", name)
	if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
		return "", fmt.Errorf("项目已存在: %s", dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "agw.toml"), []byte(agwTomlTemplate), 0o644); err != nil {
		return "", err
	}
	if runner == nil {
		runner = execRunner{}
	}
	if err := runner.Run("git", "init", dir); err != nil {
		fmt.Fprintf(os.Stderr, "警告：git init 失败（%v），项目已创建但不受版本管理\n", err)
	}

	// 令牌写入 local 配置
	cfg, err := config.Load(root)
	if err != nil {
		return "", fmt.Errorf("读取配置失败: %w", err)
	}
	token = config.NewToken()
	proj := cfg.Projects[name]
	proj.Token = token
	cfg.Projects[name] = proj
	cfg.RebuildTokenIndex()
	if err := config.SaveLocal(root, cfg); err != nil {
		return "", err
	}
	fmt.Printf("项目 %s 已创建：%s\ngit 仓库已初始化；启动：agw run claude --project %s\n", name, dir, name)
	return token, nil
}

// Item 是项目概览。
type Item struct {
	Name      string
	Branch    string
	Dirty     bool
	Providers []string
	Preferred string
	ModelMap  int // 映射条数
}

// List 列出项目与 git/覆盖摘要。
func List(root string, runner Runner) ([]Item, error) {
	if runner == nil {
		runner = execRunner{}
	}
	projDir := filepath.Join(root, "projects")
	entries, err := os.ReadDir(projDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	cfg, _ := config.Load(root)
	items := make([]Item, 0, len(names))
	for _, name := range names {
		item := Item{Name: name}
		dir := filepath.Join(projDir, name)
		if p, ok := cfg.Projects[name]; ok {
			item.Providers = p.Providers
			item.Preferred = p.Preferred
			item.ModelMap = len(p.ModelMap)
		}
		if out, err := gitOutput(dir, "branch", "--show-current"); err == nil {
			item.Branch = strings.TrimSpace(out)
		}
		if out, err := gitOutput(dir, "status", "--porcelain"); err == nil {
			item.Dirty = strings.TrimSpace(out) != ""
		}
		items = append(items, item)
	}
	return items, nil
}

// gitOutput 捕获 git 命令输出。
func gitOutput(dir string, args ...string) (string, error) {
	full := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", full...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
