// agw 是本地大模型 API 路由网关的命令行入口。
package main

import (
	"os"

	"agent_gateway/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
