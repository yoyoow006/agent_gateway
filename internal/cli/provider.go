package cli

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"agent_gateway/internal/config"
	"agent_gateway/internal/protocol/anthropic"

	"github.com/spf13/cobra"
)

var (
	providerCmd = &cobra.Command{Use: "provider", Short: "管理供应商池（写回 local.toml 并热重载）"}
	provListCmd = &cobra.Command{Use: "list", Short: "列出供应商", Run: runProviderList}
	provAddCmd  = &cobra.Command{Use: "add <名称>", Short: "新增/更新供应商", Args: cobra.ExactArgs(1), Run: runProviderAdd}
	provRmCmd   = &cobra.Command{Use: "remove <名称>", Short: "移除供应商", Args: cobra.ExactArgs(1), Run: runProviderRemove}
	provEnCmd   = &cobra.Command{Use: "enable <名称>", Short: "启用", Args: cobra.ExactArgs(1), Run: runProviderToggle(true)}
	provDisCmd  = &cobra.Command{Use: "disable <名称>", Short: "停用", Args: cobra.ExactArgs(1), Run: runProviderToggle(false)}
	provTestCmd = &cobra.Command{Use: "test <名称>", Short: "探测供应商连通性（GET /v1/models）", Args: cobra.ExactArgs(1), Run: runProviderTest}
	switchCmd   = &cobra.Command{Use: "switch <名称>", Short: "设置粘性首选供应商", Args: cobra.ExactArgs(1), Run: runSwitch}
)

func init() {
	provAddCmd.Flags().String("protocol", "", "anthropic | openai-chat | openai-responses（必填）")
	provAddCmd.Flags().String("base-url", "", "上游 base URL（必填）")
	provAddCmd.Flags().String("api-key-env", "", "密钥环境变量名（推荐，优先于 --api-key）")
	provAddCmd.Flags().String("api-key", "", "密钥明文（只写入 0600 的 local.toml）")
	provAddCmd.Flags().Int("priority", 100, "优先级，数字越小越优先")
	provAddCmd.Flags().StringArray("model", nil, "模型映射 from=to（可重复）")
	provAddCmd.Flags().StringArray("header", nil, "附加上游请求头 K=V（可重复）")
	_ = provAddCmd.MarkFlagRequired("protocol")
	_ = provAddCmd.MarkFlagRequired("base-url")

	for _, c := range []*cobra.Command{providerCmd} {
		addRootFlag(c)
	}
	for _, c := range []*cobra.Command{provListCmd, provAddCmd, provRmCmd, provEnCmd, provDisCmd, provTestCmd} {
		addRootFlag(c)
		providerCmd.AddCommand(c)
	}
	addRootFlag(switchCmd)
	rootCommands = append(rootCommands, providerCmd, switchCmd)
}

// saveAndReload 写回配置并尽力热重载。
func saveAndReload(root string, cfg *config.Config) {
	if err := config.SaveLocal(root, cfg); err != nil {
		fatalf("写回 local.toml 失败: %v", err)
	}
	reloadIfRunning(root, cfg)
}

func runProviderList(cmd *cobra.Command, args []string) {
	root := resolveRoot()
	cfg := loadConfig(root)
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "名称\t协议\t优先级\t启用\t粘性\tbase_url")
	for _, p := range cfg.Providers {
		fmt.Fprintf(tw, "%s\t%s\t%d\t%v\t%v\t%s\n", p.Name, p.Protocol, p.Priority, p.Enabled, p.Preferred, p.BaseURL)
	}
	tw.Flush()
}

func parseKVs(items []string) map[string]string {
	out := map[string]string{}
	for _, it := range items {
		kv := strings.SplitN(it, "=", 2)
		if len(kv) == 2 && kv[0] != "" {
			out[kv[0]] = kv[1]
		}
	}
	return out
}

func runProviderAdd(cmd *cobra.Command, args []string) {
	root := resolveRoot()
	cfg := loadConfig(root)
	name := args[0]
	protocol, _ := cmd.Flags().GetString("protocol")
	baseURL, _ := cmd.Flags().GetString("base-url")
	apiKeyEnv, _ := cmd.Flags().GetString("api-key-env")
	apiKey, _ := cmd.Flags().GetString("api-key")
	priority, _ := cmd.Flags().GetInt("priority")
	models, _ := cmd.Flags().GetStringArray("model")
	headers, _ := cmd.Flags().GetStringArray("header")

	if !config.Protocol(protocol).Valid() {
		fatalf("协议非法: %q（anthropic | openai-chat | openai-responses）", protocol)
	}
	if apiKeyEnv == "" && apiKey == "" {
		fatalf("需要 --api-key-env 或 --api-key 之一")
	}
	existing := cfg.Provider(name)
	np := config.Provider{
		Name: name, Protocol: config.Protocol(protocol), BaseURL: baseURL,
		APIKey: apiKey, APIKeyEnv: apiKeyEnv,
		Priority: priority, Enabled: true,
		ModelMap: parseKVs(models), Headers: parseKVs(headers),
	}
	if existing != nil {
		np.Preferred = existing.Preferred // 保留粘性标记
	}
	// 替换或追加
	replaced := false
	for i := range cfg.Providers {
		if cfg.Providers[i].Name == name {
			cfg.Providers[i] = np
			replaced = true
			break
		}
	}
	if !replaced {
		cfg.Providers = append(cfg.Providers, np)
	}
	cfg.RebuildTokenIndex()
	saveAndReload(root, cfg)
	fmt.Printf("供应商 %s 已保存（protocol=%s priority=%d）\n", name, protocol, priority)
}

func runProviderRemove(cmd *cobra.Command, args []string) {
	root := resolveRoot()
	cfg := loadConfig(root)
	name := args[0]
	if cfg.Provider(name) == nil {
		fatalf("供应商不存在: %s", name)
	}
	out := cfg.Providers[:0]
	for _, p := range cfg.Providers {
		if p.Name != name {
			out = append(out, p)
		}
	}
	cfg.Providers = out
	cfg.RebuildTokenIndex()
	saveAndReload(root, cfg)
	fmt.Printf("供应商 %s 已移除\n", name)
}

func runProviderToggle(enable bool) func(*cobra.Command, []string) {
	return func(cmd *cobra.Command, args []string) {
		root := resolveRoot()
		cfg := loadConfig(root)
		name := args[0]
		p := cfg.Provider(name)
		if p == nil {
			fatalf("供应商不存在: %s", name)
		}
		p.Enabled = enable
		cfg.RebuildTokenIndex()
		saveAndReload(root, cfg)
		fmt.Printf("供应商 %s 已%v\n", name, map[bool]string{true: "启用", false: "停用"}[enable])
	}
}

// ProbeProvider 探测供应商 /v1/models，返回延迟与错误。
func ProbeProvider(p *config.Provider) (time.Duration, error) {
	key, err := p.ResolveAPIKey()
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequest("GET", strings.TrimSuffix(p.BaseURL, "/")+"/v1/models", nil)
	if err != nil {
		return 0, err
	}
	if p.Protocol == config.ProtocolAnthropic {
		req.Header.Set("X-Api-Key", key)
		req.Header.Set("Anthropic-Version", anthropic.APIVersion)
	} else {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	for k, v := range p.Headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	start := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		return elapsed, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 400 {
		return elapsed, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return elapsed, nil
}

func runProviderTest(cmd *cobra.Command, args []string) {
	root := resolveRoot()
	cfg := loadConfig(root)
	p := cfg.Provider(args[0])
	if p == nil {
		fatalf("供应商不存在: %s", args[0])
	}
	ms, err := ProbeProvider(p)
	if err != nil {
		fatalf("❌ %s 探测失败: %v", p.Name, err)
	}
	fmt.Printf("✅ %s 可达（%s，%dms）\n", p.Name, p.Protocol, ms.Milliseconds())
}

func runSwitch(cmd *cobra.Command, args []string) {
	root := resolveRoot()
	cfg := loadConfig(root)
	name := args[0]
	if cfg.Provider(name) == nil {
		fatalf("供应商不存在: %s", name)
	}
	for i := range cfg.Providers {
		cfg.Providers[i].Preferred = cfg.Providers[i].Name == name
	}
	cfg.RebuildTokenIndex()
	saveAndReload(root, cfg)
	fmt.Printf("粘性首选已切换为 %s（健康时优先路由，减少缓存失效）\n", name)
}
