# 更新日志

本文件基于 Git 提交历史记录 UDDNS 的重要变更。

## 未发布

暂无变更。

## v1.6.0 - 2026-06-13

### 新增

- 新增高级 jobs 模式，可在同一进程中运行多个命名 DNS 更新任务。
- 每个 job 支持独立选择 provider、updater、DNS 记录、zone 和地址族。
- 新增 `verify` 模式：`auto`、`off` 和 `updater_api`。
- Cloudflare 和 Aliyun updater 支持通过 API 验证当前 DNS 记录。
- 新增 `config check`，用于在不启动调度器的情况下验证配置。

### 变更

- 集中配置加载逻辑，并支持 per-job 配置覆盖。
- 加固 systemd 安装器，升级时保留现有 service 配置。
- 扩展发布前 CI 校验。

### 修复

- 修复优雅退出处理。
- 修复 notifier、IP 和 DNS 记录无效配置的校验。

## v1.5.1 - 2026-06-03

### 修复

- 安装时如果所选配置路径需要 sudo，会给出提示。

## v1.5.0 - 2026-05-22

### 新增

- 新增 `providers.use` 和 `updaters.use`，用于显式选择 provider/updater。
- 新增中文更新日志 `CHANGELOG.zh-CN.md`。

### 变更

- 将 provider/updater 的 map 注册表替换为顺序确定的泛型 registry。
- provider/updater 构造函数不再直接依赖 Viper。
- 改进 provider/updater 配置错误处理：缺失配置会跳过，配置存在但无效会带上下文停止启动。
- 更新 README 链接，指向英文和中文更新日志。

### 修复

- 修复 LightDNS updater 的拼写和显示大小写问题，同时保持 `updaters.lightdns` 配置 key 兼容。

## v1.4.0 - 2026-05-21

### 新增

- 新增 `uddns.yaml` 中的日志配置：`logging.level`、`logging.dir` 和 `logging.retention_days`。
- 新增对应环境变量覆盖：`UDDNS_LOG_LEVEL`、`UDDNS_LOG_DIR` 和 `UDDNS_LOG_RETENTION_DAYS`。
- 新增按自然日轮转的文件日志，文件名格式为 `uddns-YYYY-MM-DD.log`。
- 新增日志保留清理，自动删除超过配置自然日天数的日志。
- 新增调度行为、日志配置和日志轮转的聚焦测试。
- 新增 `CHANGELOG.md`。
- 新增中文文档 `README.zh-CN.md`。

### 变更

- 改进应用日志，增加 provider、updater、notifier、IP 变化、更新和通知相关的结构化上下文。
- 将调度执行逻辑重构为可测试的单轮流程。
- 更新安装器，使 systemd 日志环境变量可选，默认不覆盖配置文件中的日志设置。
- 重新整理英文 README，使其覆盖当前安装、配置、日志和版本历史流程。

## v1.3.1 - 2026-05-21

### 新增

- 新增 curl 安装脚本。
- 安装器支持可选安装为 systemd 服务。
- Makefile 新增 Linux 和 Darwin 的 amd64/arm64 构建目标。

### 变更

- 简化 Cloudflare proxy 条件判断。
- 文档补充 curl 安装器和 systemd 安装方式。

## v1.3.0 - 2024-12-26

### 新增

- Cloudflare updater 新增 proxy 支持。

## v1.2.1 - 2024-09-29

### 新增

- DNS 更新失败时发送 notifier 消息。

## v1.2.0 - 2024-07-29

### 新增

- 新增 Aliyun DNS updater 支持。

## v1.1.0 - 2024-07-15

### 新增

- 新增 IPv6 支持。
- 新增 LightDNS updater 支持。

### 修复

- Cloudflare 更新失败后清理缓存的 zone 和 record ID，使后续重试可以恢复。
- 修复 provider 构造返回值，使其返回具体值。
- 修复若干拼写和 README 细节。

## v1.0.4 - 2024-06-13

### 新增

- 新增 `ip.fm` 外部 IP 服务 provider。

## v1.0.3 - 2024-06-13

### 新增

- 新增 Cloudflare API token 认证支持。

## v1.0.2 - 2024-06-13

### 新增

- IPv4 IP-service 查询的 HTTP client 强制使用 IPv4。

### 变更

- 整理 `main.go` imports。
- 合并来自 pull request #1 的外部贡献。

## v1.0.1 - 2024-05-20

### 修复

- 修复 README 中的 releases 页面链接。
- 输出不是终端时禁用彩色日志。

## v1.0.0 - 2024-05-17

### 新增

- 新增初始 GitHub Actions 和 GoReleaser release workflow。
- 新增 README，包含安装、配置、运行、支持的 providers/updaters/notifiers 等说明。
- 新增通过 `UDDNS_INTERVAL` 配置更新间隔。
- 新增多个配置文件查找位置，包括当前目录、用户配置目录和 `/etc`。
- 新增外部 IP 服务和本机网络接口 provider。
- 新增 DuckDNS updater。
- 新增 notifier 基础设施和 Telegram notifier。
- 新增应用层主更新循环。
- 新增简单 Makefile。

### 变更

- 降低 Go 版本要求。
- 重构构造函数和应用组织方式。
- 重构初始日志设置。

### 修复

- 修复从 home 目录解析配置路径的问题。
- 修复配置文件查找顺序。
- 为 RouterOS 和 DuckDNS 必填配置增加校验。
- 修复 slog key 使用和若干拼写问题。

## v1.0.0 之前

### 新增

- UDDNS 初始项目骨架。
