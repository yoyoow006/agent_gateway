# support-env-file-keys 设计

## 关键决策

| # | 决策 | 备选与弃选理由 |
|---|---|---|
| D1 | 加载点放在 `buildServer`（serve/start 共同入口）早期，`os.Setenv` 仅在变量不存在时执行 | 密钥解析（ResolveAPIKey）在每次请求时实时 `os.Getenv`，一次注入即全链路生效（含后台 start 继承、provider test）；逐请求读文件反而引入 IO 与竞态 |
| D2 | 自写 ~40 行极简解析器（KEY=VALUE、`#` 注释、空行、成对引号剥除、可选 `export ` 前缀） | 引第三方 dotenv 库违反"依赖最小"红线（现仅 cobra/toml）；插值等高级特性非目标 |
| D3 | 真实环境变量优先（不覆盖已存在值） | dotenv 惯例；保证容器/systemd EnvironmentFile 等显式注入不被文件悄悄覆盖 |
| D4 | 语法错误带行号快速失败 | 与"配置语法错误启动快速失败"的既有失败路径一致；静默跳过会让密钥缺失变成运行期模糊 401 |
| D5 | 位置固定 `<根>/.env`；`.gitignore` 追加 `/.env`；提交 `.env.example` | 根目录是 dotenv 惯例；不加忽略会直接违反密钥不进 git 红线 |
| D6 | 权限宽于 0600 仅警告不阻止 | 工具不擅改用户文件；0600 建议写入 README 与 example 注释 |

## 失败路径

- `.env` 不存在：正常，无日志噪音。
- 语法错误：启动失败，错误含 `行号: 原文`。
- `.env` 有变量但配置引用了未定义的 `api_key_env`：沿用既有报错（"环境变量 X 未设置"）。
- 权限过宽（group/other 可读）：警告日志，继续加载。

## 安全

- `.env` 入 gitignore；example 模板不含真实密钥。
- 加载发生在进程内，不回写、不外发；日志不打印值。
- 优先级链：local.toml 明文 `api_key` > 真实环境变量 > `.env`。

## 验证

- 解析器单测：注释/空行/引号/export 前缀/缺 `=` 报错/不覆盖已有环境。
- 集成测试：临时根写 `.env` + `api_key_env` 供应商 → buildServer 后请求经 httptest 假上游断言认证头正确。
- 回归：无 `.env` 时全量套件不变绿。
