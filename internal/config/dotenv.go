package config

import (
	"fmt"
	"os"
	"strings"
)

// ParseEnvFile 解析 .env 内容：每行 KEY=VALUE，支持 # 注释、空行、
// 成对单双引号剥除与可选 export 前缀；不做变量插值。
// 语法错误（缺 = 或空变量名）返回带行号的错误。
func ParseEnvFile(data []byte) (map[string]string, error) {
	out := map[string]string{}
	for i, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		eq := strings.Index(line, "=")
		if eq < 0 {
			return nil, fmt.Errorf(".env 第 %d 行缺少 '=': %s", i+1, raw)
		}
		key := strings.TrimSpace(line[:eq])
		if key == "" {
			return nil, fmt.Errorf(".env 第 %d 行缺少变量名", i+1)
		}
		val := strings.TrimSpace(line[eq+1:])
		if len(val) >= 2 {
			first, last := val[0], val[len(val)-1]
			if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		out[key] = val
	}
	return out, nil
}

// LoadEnvFile 把 .env 注入进程环境（已存在的真实环境变量不覆盖，dotenv 惯例）；
// 文件不存在返回 (nil, nil)。返回权限警告（若有，宽于 0600）供调用方记日志。
func LoadEnvFile(path string) (warning string, err error) {
	fi, statErr := os.Stat(path)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return "", nil
		}
		return "", statErr
	}
	if fi.Mode().Perm()&0o077 != 0 {
		warning = fmt.Sprintf("%s 权限 %v 宽于 0600，密钥有被同机其他用户读取的风险，建议 chmod 600", path, fi.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return warning, err
	}
	vars, err := ParseEnvFile(data)
	if err != nil {
		return warning, err
	}
	for k, v := range vars {
		if _, exists := os.LookupEnv(k); !exists {
			os.Setenv(k, v)
		}
	}
	return warning, nil
}
