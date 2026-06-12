# UDDNS

UDDNS 是一个轻量的动态 DNS 更新器。它会从 provider 获取当前公网 IP，
再通过 updater 更新 DNS 记录，并且可以在 IP 变化或更新失败时发送通知。

[English README](README.md) | [更新日志](CHANGELOG.zh-CN.md)

## 功能

- 支持 IPv4 和 IPv6。
- Provider：RouterOS、外部 IP 服务、本机网络接口。
- Updater：Cloudflare、Aliyun、DuckDNS、LightDNS。
- Notifier：Telegram。
- 支持通过环境变量配置更新间隔。
- 结构化日志，支持按自然日轮转文件日志和保留天数清理。
- 支持 curl 安装器，并可选择安装为 systemd 服务。
- 使用 GoReleaser 发布多平台二进制文件。

## 安装

使用 curl 安装最新版本：

```shell
curl -fsSL https://raw.githubusercontent.com/we11adam/uddns/master/install.sh | sh
```

安装器会检测 systemd，并询问是否安装为 systemd 服务。非交互安装 systemd 服务：

```shell
curl -fsSL https://raw.githubusercontent.com/we11adam/uddns/master/install.sh | sh -s -- --systemd --config /etc/uddns.yaml
```

常用安装参数：

```shell
--version <tag>              安装指定 release tag
--install-dir <dir>          安装目录，默认 /usr/local/bin
--systemd                    安装或更新 systemd 服务
--no-systemd                 跳过 systemd 服务安装
--config <path>              systemd 服务使用的配置文件路径
--interval <duration>        systemd 服务使用的 UDDNS_INTERVAL
--log-dir <dir>              为 systemd 服务启用轮转文件日志
--log-retention-days <n>     日志保留的自然日天数
```

也可以使用 Go 安装：

```shell
go install github.com/we11adam/uddns@latest
```

或者从 [releases 页面](https://github.com/we11adam/uddns/releases/)下载二进制文件。

## 配置

UDDNS 会按以下顺序查找配置文件：

1. `-c` 指定的路径。
2. `UDDNS_CONFIG`。
3. `./uddns.yaml`。
4. `~/.config/uddns.yaml`。
5. `/etc/uddns.yaml`。

示例：

```yaml
providers:
  use: ip_service
  routeros:
    endpoint: https://192.168.88.1
    username: admin
    password: ""
    insecure: true
  ip_service:
    - ifconfig.me
    - ip.fm
  netif:
    name: ppp0

updaters:
  use: cloudflare
  cloudflare:
    apitoken: your-cloudflare-api-token
    # 也可以使用 email + apikey:
    # email: user@example.com
    # apikey: your-cloudflare-api-key
    domain: ddns.example.com
    proxy: http://127.0.0.1:2080
  aliyun:
    accesskeyid: your-access-key-id
    accesskeysecret: your-access-key-secret
    domain: ddns.example.com
    regionid: cn-hangzhou
  duckdns:
    token: your-duckdns-token
    domain: your-subdomain
  lightdns:
    key: your-lightdns-key
    domain: ddns.example.com

notifiers:
  use: telegram
  telegram:
    chat_id: -1001234567890
    token: 1234567890:telegram-bot-token
    proxy: http://127.0.0.1:2080

logging:
  level: info
  dir: /var/log/uddns
  retention_days: 7
```

至少需要配置一个 provider 和一个 updater。如果同时配置多个 provider 或 updater，
建议用 `providers.use` 和 `updaters.use` 明确选择。未设置 `use` 时，UDDNS 会按确定的
registry 顺序检查已配置项。如果某个已配置的 provider 或 updater 配置错误，程序会直接
报出该配置错误，而不是静默回退到其他配置。
notifier 是可选的；配置 notifier 时可以用 `notifiers.use` 明确选择，配置错误会让程序
在启动时失败。

### Providers

- `routeros`：从 MikroTik RouterOS 设备读取 IP。
  - `endpoint`：RouterOS API 地址。
  - `username`：RouterOS 用户名。
  - `password`：RouterOS 密码。
  - `insecure`：跳过 TLS 校验。可选，默认 `true`。
- `ip_service`：从外部服务读取公网 IP。
  - 支持：`ip.fm`、`ifconfig.me`、`ip.sb`、`3322.org`。
- `netif`：从本机网络接口读取 IP。
  - `name`：网络接口名称。

### Updaters

- `cloudflare`：
  - `apitoken`：Cloudflare API Token。
  - `email` 和 `apikey`：另一种 Cloudflare API Key 认证方式。
  - `domain`：需要更新的 DNS 记录，例如 `ddns.example.com`。
  - `proxy`：可选 HTTP 代理。
- `aliyun`：
  - `accesskeyid`：阿里云 AccessKey ID。
  - `accesskeysecret`：阿里云 AccessKey Secret。
  - `domain`：需要更新的 DNS 记录。
  - `regionid`：可选，默认 `cn-hangzhou`。
- `duckdns`：
  - `token`：DuckDNS token。
  - `domain`：DuckDNS 子域名，不包含 `.duckdns.org`。
- `lightdns`：
  - `key`：LightDNS DDNS key。
  - `domain`：需要更新的 DNS 记录。

### Notifiers

- `telegram`：
  - `token`：Telegram bot token。
  - `chat_id`：Telegram chat ID。
  - `proxy`：可选 HTTP 代理。

## 运行

直接运行：

```shell
uddns -c /etc/uddns.yaml
```

后台运行：

```shell
nohup uddns -c /etc/uddns.yaml > uddns.log 2>&1 &
```

默认更新间隔是 `30s`。可以通过 `UDDNS_INTERVAL` 覆盖：

```shell
UDDNS_INTERVAL=5m uddns -c /etc/uddns.yaml
```

## 日志

日志可以在 `uddns.yaml` 中配置：

```yaml
logging:
  level: info
  dir: /var/log/uddns
  retention_days: 7
```

- `level`：`debug`、`info`、`warn` 或 `error`。默认 `info`。
- `dir`：设置后启用文件日志。不设置时只输出到 stdout。
- `retention_days`：保留多少个自然日的日志，包含今天。默认 `7`。

文件日志按本地自然日轮转，文件名格式：

```text
uddns-YYYY-MM-DD.log
```

环境变量会覆盖配置文件中的日志设置：

- `UDDNS_LOG_LEVEL`
- `UDDNS_LOG_DIR`
- `UDDNS_LOG_RETENTION_DAYS`

## Roadmap

- [ ] 增加更多 providers。
- [ ] 增加更多 updaters。
- [ ] 增加更细粒度的配置项。
- [ ] 增加 Dockerfile。
- [ ] 增加 systemd 安装之外的 daemon 模式。
- [x] 增加更清晰的日志。
- [x] 增加测试。
- [x] 增加 CI/CD release workflow。
- [x] 增加 curl 安装器。
- [x] 增加 systemd 服务安装器。

## 贡献

欢迎提交 Pull Request。较大的改动建议先开 issue 说明计划。

## License

MIT
