package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

var (
	startCmd  = &cobra.Command{Use: "start", Short: "后台启动网关（分离进程）", Run: runStart}
	stopCmd   = &cobra.Command{Use: "stop", Short: "停止网关（SIGTERM 优雅退出）", Run: runStop}
	statusCmd = &cobra.Command{Use: "status", Short: "查看运行状态与供应商健康", Run: runStatus}
	logsCmd   = &cobra.Command{Use: "logs", Short: "查看网关日志（-f 跟随）", Run: runLogs}
)

func init() {
	addRootFlag(startCmd)
	addRootFlag(stopCmd)
	addRootFlag(statusCmd)
	addRootFlag(logsCmd)
	logsCmd.Flags().BoolP("follow", "f", false, "持续跟随输出")
	statusCmd.Flags().Bool("json", false, "JSON 输出")
	rootCommands = append(rootCommands, startCmd, stopCmd, statusCmd, logsCmd)
}

// readPid 读取 pidfile，无或损坏返回 0。
func readPid(root string) int {
	data, err := os.ReadFile(pidPath(root))
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	return pid
}

// pidAlive 报告进程是否存活（信号 0 探测）。
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// pidIdentity 记录后台网关进程身份，避免 PID 复用误杀。
type pidIdentity struct {
	Pid       int    `json:"pid"`
	StartTime int64  `json:"start_time"`
	Exe       string `json:"exe"`
	Root      string `json:"root"`
}

// readPidIdentity 读取 JSON 身份；legacy 表示旧版纯 PID 格式。
func readPidIdentity(root string) (identity pidIdentity, isJSON bool, err error) {
	data, err := os.ReadFile(pidPath(root))
	if err != nil {
		if os.IsNotExist(err) {
			return pidIdentity{}, false, nil
		}
		return pidIdentity{}, false, err
	}
	text := strings.TrimSpace(string(data))
	if strings.HasPrefix(text, "{") {
		if err := json.Unmarshal([]byte(text), &identity); err != nil {
			return pidIdentity{}, false, err
		}
		return identity, identity.Pid > 0, nil
	}
	return pidIdentity{}, false, nil
}

// writePidIdentity 以 0600 写入 JSON 身份 pidfile。
func writePidIdentity(root string, identity pidIdentity) error {
	data, err := json.Marshal(identity)
	if err != nil {
		return err
	}
	return os.WriteFile(pidPath(root), data, 0o600)
}

// processIdentity 从 Linux /proc 读取进程启动时间与可执行路径。
func processIdentity(pid int) (startTime int64, exe string, ok bool) {
	if pid <= 0 {
		return 0, "", false
	}
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, "", false
	}
	// comm 可能含空格/括号；从最后一次 ')' 后切分字段 3 开始。
	text := string(data)
	if idx := strings.LastIndex(text, ")"); idx < 0 {
		return 0, "", false
	} else {
		text = text[idx+2:]
	}
	fields := strings.Fields(text)
	// fields[0]=state(3), fields[19]=starttime(22).
	if len(fields) < 20 {
		return 0, "", false
	}
	startTime, err = strconv.ParseInt(fields[19], 10, 64)
	if err != nil {
		return 0, "", false
	}
	exe, err = os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "exe"))
	if err != nil {
		return 0, "", false
	}
	return startTime, exe, true
}

// sameGatewayProcess 校验 pidfile 身份是否仍属于同一网关进程。
func sameGatewayProcess(identity pidIdentity) bool {
	startTime, exe, ok := processIdentity(identity.Pid)
	return ok && startTime == identity.StartTime && exe == identity.Exe
}

// gatewayRunning 表示存在身份匹配的运行中网关。
func gatewayRunning(root string) bool {
	identity, json, err := readPidIdentity(root)
	return err == nil && json && sameGatewayProcess(identity)
}

// selfPath 可注入的可执行文件路径（测试替换）。
var selfPath = func() (string, error) { return os.Executable() }

var healthReady = func(root, listen string) bool {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return false
	}
	resp, err := http.Get("http://" + net.JoinHostPort(host, port) + "/__agw/healthz")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

var startReadyTimeout = 5 * time.Second

// logTail 返回日志最后几行（错误提示用）。
func logTail(root string, n int) string {
	data, err := os.ReadFile(logPath(root))
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " | ")
}

// StartGateway 分离启动 `agw serve`；已运行则报错。返回 pid。
// 通过 Wait 通道判定子进程是否站稳（僵尸进程对 signal-0 探测是存活的，
// 不能用 pidAlive 判断启动成败——端口占用时子进程会秒退成僵尸）。
func StartGateway(root string, cfgListen string) (int, error) {
	if gatewayRunning(root) {
		identity, _, _ := readPidIdentity(root)
		return 0, fmt.Errorf("网关已在运行（pid %d）；如需重启先 agw stop", identity.Pid)
	}
	if err := os.MkdirAll(runDir(root), 0o755); err != nil {
		return 0, err
	}
	logFile, err := os.OpenFile(logPath(root), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return 0, err
	}
	defer logFile.Close()
	self, err := selfPath()
	if err != nil {
		return 0, err
	}
	cmd := exec.Command(self, "serve", "--root", root)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // 脱离会话，父进程退出不影响
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }() // 同时负责收尸，避免僵尸

	// healthz 成功前不写最终 pidfile；退出优先判定失败。
	timeout := time.After(startReadyTimeout)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-waitCh:
			os.Remove(pidPath(root))
			return pid, startExitError(root)
		case <-timeout:
			return pid, fmt.Errorf("网关启动超时：healthz 在 %s 内未就绪（完整日志 %s）", startReadyTimeout, logPath(root))
		case <-ticker.C:
			if healthReady(root, cfgListen) {
				startTime, exe, ok := processIdentity(pid)
				if !ok {
					os.Remove(pidPath(root))
					return pid, fmt.Errorf("读取网关进程身份失败，不能安全记录 pidfile: %d", pid)
				}
				if err := writePidIdentity(root, pidIdentity{Pid: pid, StartTime: startTime, Exe: exe, Root: root}); err != nil {
					return pid, err
				}
				return pid, nil
			}
		}
	}
}

func startExitError(root string) error {
	tail := logTail(root, 3)
	if tail != "" {
		return fmt.Errorf("网关启动失败：%s（完整日志 %s）", tail, logPath(root))
	}
	return fmt.Errorf("网关启动后立即退出，请查看日志: %s", logPath(root))
}

func runStart(cmd *cobra.Command, args []string) {
	root := resolveRoot()
	cfg := loadConfig(root)
	pid, err := StartGateway(root, cfg.Gateway.Listen)
	if err != nil {
		fatalf("%v", err)
	}
	fmt.Printf("网关已启动：pid=%d 监听=%s 日志=%s\n", pid, cfg.Gateway.Listen, logPath(root))
}

// StopGateway 校验进程身份后发送 SIGTERM；超时返回错误。
func StopGateway(root string) error {
	identity, jsonFormat, err := readPidIdentity(root)
	if err != nil {
		return err
	}
	if !jsonFormat {
		// 兼容发现旧格式，但绝不基于纯 PID 发送信号。
		if readPid(root) > 0 {
			_ = os.Remove(pidPath(root))
			return fmt.Errorf("旧 pidfile 缺少进程身份，拒绝停止；请重启网关生成新 pidfile，或确认后删除 %s", pidPath(root))
		}
		_ = os.Remove(pidPath(root))
		return fmt.Errorf("网关未在运行")
	}
	if !sameGatewayProcess(identity) {
		_ = os.Remove(pidPath(root))
		return fmt.Errorf("PID %d 身份不匹配，可能已被复用；已清理 pidfile，未发送信号", identity.Pid)
	}
	proc, _ := os.FindProcess(identity.Pid)
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("发送 SIGTERM 失败: %w", err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if !pidAlive(identity.Pid) {
			os.Remove(pidPath(root))
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("pid %d 在 15s 内未退出（可能在排空长流），可稍后重试或手动 kill", identity.Pid)
}

func runStop(cmd *cobra.Command, args []string) {
	root := resolveRoot()
	if err := StopGateway(root); err != nil {
		fatalf("%v", err)
	}
	fmt.Println("网关已停止")
}

// StatusInfo 是 status 的结构化输出。
type StatusInfo struct {
	Running   bool             `json:"running"`
	Pid       int              `json:"pid"`
	Listen    string           `json:"listen,omitempty"`
	UptimeSec int              `json:"uptime_sec,omitempty"`
	Providers []map[string]any `json:"providers,omitempty"`
}

func runStatus(cmd *cobra.Command, args []string) {
	root := resolveRoot()
	cfg := loadConfig(root)
	pid := readPid(root)
	info := StatusInfo{Pid: pid, Running: gatewayRunning(root)}
	if info.Running {
		if resp, err := adminRequest("GET", adminURL(cfg, "/__agw/metrics"), cfg); err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == 200 {
				var m struct {
					Listen    string           `json:"listen"`
					UptimeSec int              `json:"uptime_sec"`
					Providers []map[string]any `json:"providers"`
				}
				if json.Unmarshal(body, &m) == nil {
					info.Listen = m.Listen
					info.UptimeSec = m.UptimeSec
					info.Providers = m.Providers
				}
			}
		}
	}
	jsonOut, _ := cmd.Flags().GetBool("json")
	if jsonOut {
		out, _ := json.MarshalIndent(info, "", "  ")
		fmt.Println(string(out))
		return
	}
	if !info.Running {
		fmt.Printf("网关未运行（pidfile=%d 已失效）\n", pid)
		return
	}
	fmt.Printf("运行中：pid=%d 监听=%s 已运行 %ds\n", info.Pid, info.Listen, info.UptimeSec)
	if len(info.Providers) == 0 {
		return
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "供应商\t状态\t请求\t失败\t在途\t最近错误")
	for _, p := range info.Providers {
		fmt.Fprintf(tw, "%v\t%v\t%v\t%v\t%v\t%v\n",
			p["Name"], p["State"], p["Requests"], p["Failures"], p["InFlight"], p["LastErr"])
	}
	tw.Flush()
}

func runLogs(cmd *cobra.Command, args []string) {
	root := resolveRoot()
	follow, _ := cmd.Flags().GetBool("follow")
	path := logPath(root)
	if _, err := os.Stat(path); err != nil {
		fatalf("日志不存在: %s（网关从未启动过？）", path)
	}
	if !follow {
		f, err := os.Open(path)
		if err != nil {
			fatalf("%v", err)
		}
		defer f.Close()
		io.Copy(os.Stdout, f)
		return
	}
	// 简易 tail -f
	f, err := os.Open(path)
	if err != nil {
		fatalf("%v", err)
	}
	defer f.Close()
	f.Seek(0, io.SeekEnd)
	buf := make([]byte, 32*1024)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			os.Stdout.Write(buf[:n])
		}
		if err == io.EOF {
			time.Sleep(300 * time.Millisecond)
			continue
		}
		if err != nil {
			fatalf("%v", err)
		}
	}
}
