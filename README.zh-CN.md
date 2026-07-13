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
- 使用 GoReleaser 发布多平台二进制文件，并提供 SBOM 和 GitHub Actions
  来源证明。

## 安装

使用 curl 安装最新版本：

```shell
curl -fsSL https://github.com/we11adam/uddns/releases/latest/download/install.sh | sh
```

如需验证安装来源，请先下载脚本、校验其 GitHub Actions 来源证明，再执行：

```shell
curl -fsSLO https://github.com/we11adam/uddns/releases/latest/download/install.sh
gh attestation verify install.sh --repo we11adam/uddns
sh install.sh
```

官方发布归档仅包含对应平台的可执行文件。安装器仅通过 HTTPS 下载 release archive，
校验 checksum，并在解压前验证 archive 内容。每个 release archive 还会附带 SBOM 和
GitHub Actions 来源证明；手动下载后可用以下命令验证：

```shell
gh attestation verify /path/to/archive --repo we11adam/uddns
```

安装器会检测 systemd，并询问是否安装为 systemd 服务。生成的 unit 会以专用的非特权
`uddns` 用户运行，并通过受保护的 systemd credential 传入配置，因此源配置文件可以继续
由 root 持有并使用 `0600` 权限。非交互安装 systemd 服务：

```shell
curl -fsSL https://github.com/we11adam/uddns/releases/latest/download/install.sh | sh -s -- --systemd --config /etc/uddns.yaml
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

安装器接收的安装、配置和日志路径必须是绝对路径。如果所选配置缺失，或者不是可读的普通
文件，systemd unit 只会被启用，不会启动。

也可以使用 Go 1.26.5 或更新版本从源码安装：

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

通过 `-c` 或 `UDDNS_CONFIG` 显式指定的路径不可读时会直接报错，不会回退到其他位置。
UDDNS 不会自动加载 `.env` 文件；请通过 shell 或服务管理器设置环境变量。在 Unix 上，
所选配置文件不能向所属组或其他用户开放任何权限。启动 UDDNS 前请收紧权限：

```shell
chmod 600 /etc/uddns.yaml
```

### 简单模式

如果一个 UDDNS 进程只更新一条 DNS 记录，使用简单模式即可。这是原有配置格式，
会继续完整支持。

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
    # 可选。DNS zone 无法从最后两段推断时设置。
    # zone: example.com
    proxy: http://127.0.0.1:2080
  aliyun:
    accesskeyid: your-access-key-id
    accesskeysecret: your-access-key-secret
    domain: ddns.example.com
    # 可选。DNS zone 无法从最后两段推断时设置。
    # zone: example.com
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

# 可选。auto 会在 updater 支持时使用 updater API 验证。
verify: auto

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

### 高级 Jobs 模式

如果一个进程需要更新多条 DNS 记录、使用不同 DNS 服务，或者只更新指定地址族，可以使用
jobs 模式。provider 只描述如何获取 IP，updater 只描述如何访问 DNS 服务，每个 job
负责把一个 provider、一个 updater 和一条 DNS 记录连接起来。

```yaml
providers:
  ip_service:
    - ifconfig.me
    - ip.fm
  netif:
    name: ppp0

updaters:
  cloudflare:
    apitoken: your-cloudflare-api-token
    # proxy: http://127.0.0.1:2080
  duckdns:
    token: your-duckdns-token

jobs:
  - name: home-cloudflare
    provider: ip_service
    updater: cloudflare
    record: home.example.com
    # 可选。DNS zone 无法从最后两段推断时设置。
    zone: example.com
    families: [ipv4, ipv6]
    verify: updater_api

  - name: nas-duckdns
    provider: netif
    updater: duckdns
    record: your-subdomain
    families: [ipv4]
    verify: off

notifiers:
  use: telegram
  telegram:
    chat_id: -1001234567890
    token: 1234567890:telegram-bot-token

logging:
  level: info
  dir: /var/log/uddns
  retention_days: 7
```

job 字段：

- `name`：可选的唯一任务名。不设置时默认为 `job-<n>`。
- `provider`：要使用的 provider，例如 `ip_service`、`routeros` 或 `netif`。
- `updater`：要使用的 updater，例如 `cloudflare`、`aliyun`、`duckdns` 或 `lightdns`。
- `record`：需要更新的 DNS 记录。DuckDNS 使用不包含 `.duckdns.org` 的子域名。
- `zone`：Cloudflare 和 Aliyun 可选的 DNS zone 覆盖。
- `families`：可选地址族。支持 `ipv4` 和 `ipv6`；不设置时更新两者。
- `verify`：可选验证模式。支持 `auto`、`off` 和 `updater_api`；不设置时为 `auto`。

存在 `jobs` 时，每个 job 都有独立的 last IPv4/IPv6 状态，并按全局更新间隔顺序执行。
没有 `jobs` 时，UDDNS 会按简单模式配置运行一个隐式的 `default` job。命名 job 发出的
通知会自动带上 job 名作为前缀。

瞬时公网 IP 服务和 DNS 更新请求会进行重试。provider、严格验证或 updater 连续失败时，
只会对受影响的 job 应用带抖动的指数退避，其他 jobs 会继续运行。

job 选择的是 provider/updater 的实现名。使用同一个实现的多个 job 会共享该实现的配置；
例如所有 `cloudflare` job 都会使用 `updaters.cloudflare` 里的凭据。

verify 行为：

- `auto`：当所选 updater 支持时，用 updater API 验证当前 DNS 记录；否则跳过验证。
- `off`：更新前不验证 DNS 记录，只按本地 last IP 判断。
- `updater_api`：强制通过所选 updater 对应的 DNS 服务商 API 查询当前记录。Cloudflare
  和 Aliyun 支持该模式。DuckDNS 和 LightDNS 不支持，所以与 `verify: updater_api`
  一起使用时 `config check` 会失败。

启用 updater API 验证后，只要探测到的 IP 与 job 上次成功 IP 不同，或者 updater API
返回的当前 DNS 记录与探测 IP 不匹配，UDDNS 就会更新。启动时如果记录已经匹配，会直接
初始化本地状态，避免不必要的重写。`auto` 会在启动时、provider IP 变化时以及稳定运行期间
周期性验证，并且只查询已配置的地址族。`auto` 验证暂时失败不会阻塞由 provider IP 变化
触发的更新；`updater_api` 仍是严格模式，每轮都会验证，验证失败时会跳过该 job 的当前
循环。

### Providers

- `routeros`：从 MikroTik RouterOS 设备读取 IP。
  - `endpoint`：RouterOS API 地址。
  - `username`：RouterOS 用户名。
  - `password`：RouterOS 密码。
  - `insecure`：跳过 TLS 校验。可选，默认 `true`。
- `ip_service`：从外部服务读取公网 IP。
  - 支持：`ip.fm`、`ifconfig.me`、`ip.sb`、`3322.org`。
  - 仅接受可在公网路由的地址。
- `netif`：从本机网络接口读取 IP。
  - `name`：网络接口名称。

### Updaters

- `cloudflare`：
  - `apitoken`：Cloudflare API Token。
  - `email` 和 `apikey`：另一种 Cloudflare API Key 认证方式。
  - `domain`：需要更新的 DNS 记录，例如 `ddns.example.com`。
  - `zone`：可选 DNS zone，例如 `example.co.uk`。
  - `proxy`：可选 HTTP 或 HTTPS 代理。
- `aliyun`：
  - `accesskeyid`：阿里云 AccessKey ID。
  - `accesskeysecret`：阿里云 AccessKey Secret。
  - `domain`：需要更新的 DNS 记录。
  - `zone`：可选 DNS zone，例如 `example.co.uk`。
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
  - `proxy`：可选 HTTP 或 HTTPS 代理。

Cloudflare 和 Telegram 的代理地址必须是包含主机名的绝对 URL。支持在 URL userinfo
中提供代理凭据；除 `/` 外不得包含其他路径，也不得包含 query 或 fragment。

## 运行

直接运行：

```shell
uddns -c /etc/uddns.yaml
```

只检查配置、不启动更新循环：

```shell
uddns config check -c /etc/uddns.yaml
```

后台运行：

```shell
nohup uddns -c /etc/uddns.yaml > uddns.log 2>&1 &
```

默认更新间隔是 `30s`。`UDDNS_INTERVAL` 接受 Go duration 格式，范围为 `10s` 至
`24h`；格式错误或超出范围时会记录警告并回退到 `30s`：

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

UDDNS 创建的日志目录权限为 `0700`，日志文件权限为 `0600`，并会拒绝符号链接或非普通
文件日志目标。

## 更新日志

发布历史和未发布变更见 [CHANGELOG.zh-CN.md](CHANGELOG.zh-CN.md)，英文版本见
[CHANGELOG.md](CHANGELOG.md)。

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
