package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
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

// selfPath 可注入的可执行文件路径（测试替换）。
var selfPath = func() (string, error) { return os.Executable() }

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
	if pidAlive(readPid(root)) {
		return 0, fmt.Errorf("网关已在运行（pid %d）；如需重启先 agw stop", readPid(root))
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
	if err := os.WriteFile(pidPath(root), []byte(strconv.Itoa(pid)), 0o600); err != nil {
		return pid, err
	}
	// 观察窗口：启动失败（端口占用等）会在窗口内退出
	select {
	case <-waitCh:
		os.Remove(pidPath(root))
		tail := logTail(root, 3)
		if tail != "" {
			return pid, fmt.Errorf("网关启动失败：%s（完整日志 %s）", tail, logPath(root))
		}
		return pid, fmt.Errorf("网关启动后立即退出，请查看日志: %s", logPath(root))
	case <-time.After(700 * time.Millisecond):
		return pid, nil
	}
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

// StopGateway 发送 SIGTERM 并等待退出；超时返回错误。
func StopGateway(root string) error {
	pid := readPid(root)
	if !pidAlive(pid) {
		os.Remove(pidPath(root))
		return fmt.Errorf("网关未在运行")
	}
	proc, _ := os.FindProcess(pid)
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("发送 SIGTERM 失败: %w", err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if !pidAlive(pid) {
			os.Remove(pidPath(root))
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("pid %d 在 15s 内未退出（可能在排空长流），可稍后重试或手动 kill", pid)
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
	info := StatusInfo{Pid: pid, Running: pidAlive(pid)}
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
