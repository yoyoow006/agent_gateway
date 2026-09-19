// agw 是本地大模型 API 路由网关的命令行入口。
package main

import (
	"errors"
	"os"

	"agent_gateway/internal/cli"
)

func main() {
	err := cli.Execute()
	if err == nil {
		return
	}
	// `agw reload` 在网关拒绝 reload 时返回 ErrReloadFailed，区分于本地解析失败。
	if errors.Is(err, cli.ErrReloadFailed) {
		os.Exit(2)
	}
	os.Exit(1)
}
