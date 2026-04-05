# APK Cache 统一流水线重构说明

## 背景

这次重构的目标，是把 APK Cache 从“按功能堆叠”的实现方式，收敛成一个围绕 APT/APK 代理与缓存的统一内核。

重构前的主要问题：

- `cmd/apk-cache` 中存在大量全局状态和隐式依赖，启动流程过长，模块边界不清晰。
- APK、APT、通用代理三条链路各自维护了一套请求处理逻辑，缓存、上游访问、条件请求、完整性校验等能力重复分散。
- `internal/config`、`internal/upstream` 等新模块已经出现，但并没有真正成为运行时主干。
- 管理后台、旧配置模型、缓存策略、配额、限流等外围能力和核心代理链路耦合过深，导致继续演进成本很高。

这次重构直接以“新内核替换旧主流程”为目标，不再兼容旧的 CLI 和旧配置结构。

## 重构结果

重构后，服务只保留三类主能力：

- APK 缓存代理
- APT 缓存代理
- 通用 HTTP/HTTPS 代理
  - 主要服务于 HTTPS APT 场景
  - 是否开启由配置决定

运行时的主干现在是：

- [`cmd/apk-cache/main.go`](../cmd/apk-cache/main.go)
  - 只负责读取配置和启动应用
- [`cmd/apk-cache/app.go`](../cmd/apk-cache/app.go)
  - 负责依赖组装和 HTTP 服务启动
- [`cmd/apk-cache/pipeline.go`](../cmd/apk-cache/pipeline.go)
  - 统一请求流水线
- [`cmd/apk-cache/protocol.go`](../cmd/apk-cache/protocol.go)
  - APK、APT、Proxy 三个协议适配器
- [`cmd/apk-cache/apt_index_service.go`](../cmd/apk-cache/apt_index_service.go)
  - APT 索引解析与 hash 校验
- [`internal/config/config.go`](../internal/config/config.go)
  - 唯一配置入口
- [`internal/upstream/upstream.go`](../internal/upstream/upstream.go)
  - APK 上游管理和 failover

旧的 `cmd/apk-cache/cache.go`、`cmd/apk-cache/cache_apt.go`、`cmd/apk-cache/http_proxy.go`、`cmd/apk-cache/upstream.go` 等主流程文件已经删除。

## 新架构概览

### 1. App 组装层

`App` 是新的运行时根对象，负责初始化以下依赖：

- 配置对象
- HTTP client 工厂
- 内存缓存
- 文件锁管理器
- APK upstream manager / fetcher
- APT index service
- proxy adapter
- pipeline

这层只解决“对象如何组装”，不直接处理协议细节和缓存逻辑。

### 2. 统一请求流水线

`Pipeline` 统一了三类请求的处理流程：

1. 选择 adapter
2. 规范化请求
3. 计算缓存策略
4. 计算缓存键
5. 先查内存缓存
6. 再查磁盘缓存
7. 校验磁盘缓存
8. 未命中则访问上游
9. 流式回写客户端和缓存文件
10. 保存必要的元数据

这条流水线把此前散落在多个 handler 文件中的重复逻辑收敛到了一个地方。

### 3. 协议适配器

`ProtocolAdapter` 是这次重构的核心抽象。

当前接口包含：

- `Match(*http.Request) bool`
- `Normalize(*http.Request) (*NormalizedRequest, error)`
- `CachePolicy(*NormalizedRequest) CacheDecision`
- `CacheKey(*NormalizedRequest) (string, error)`
- `ValidateCached(...) error`
- `ValidateFetched(...) error`
- `Fetch(...) (*http.Response, error)`

目前实现了三个适配器：

- `APKAdapter`
  - 识别 Alpine APK 请求
  - 生成 APK 缓存路径
  - 使用 `internal/upstream` 获取上游内容
- `APTAdapter`
  - 识别 APT 请求
  - 按 `host + path` 生成缓存键
  - 调用 `APTIndexService` 做 `by-hash` 和 `.deb` 校验
- `ProxyAdapter`
  - 处理非 APK/APT 的 HTTP 代理请求
  - 支持 HTTPS `CONNECT`
  - 默认透传，不缓存非包类流量

### 4. APT 元数据服务

APT 的特殊性在于：

- 缓存键必须带 host，避免不同源相同路径互相污染
- `by-hash` 请求必须验证 URL 中声明的 hash
- `.deb` 包可以依赖索引中的 SHA256 做完整性验证

因此重构后将这部分从 handler 中拆出，形成独立的 `APTIndexService`：

- 启动时扫描 `cache/apt`
- 解析 `Release`、`InRelease`、`Packages*`
- 建立缓存路径到 hash 的映射
- 支持 `ValidateByHash`
- 支持 `ValidateDeb`

## 新配置模型

新版本只保留 `-config` 参数，其他运行配置全部通过 TOML 提供。

当前配置结构见 [`config.example.toml`](../config.example.toml)，主要分为：

- `[server]`
- `[[upstreams]]`
- `[cache]`
- `[cache.memory]`
- `[transport]`
- `[apk]`
- `[apt]`
- `[proxy]`

几个关键点：

- `upstreams` 当前主要服务 APK 链路，`kind = "apk"`。
- APT 通过客户端请求中的真实目标 URL 访问上游，而不是走固定 upstream 列表。
- `proxy.enabled` 和 `proxy.allow_connect` 控制通用代理能力。
- `proxy.upstream_proxy` 仅作用于 `ProxyAdapter`，用于为绝对 URL 请求和 `CONNECT` 隧道指定 `socks5://`、`http://` 或 `https://` 上游代理。
- `proxy.cache_non_package_requests` 默认 `false`，避免通用代理流量污染缓存。

## 请求流说明

### APK 请求

典型路径示例：

- `/alpine/v3.20/main/x86_64/APKINDEX.tar.gz`
- `/alpine/v3.20/main/x86_64/busybox-1.36.1-r0.apk`

处理流程：

1. `APKAdapter` 识别请求
2. 直接用路径生成缓存键
3. 先查内存，再查磁盘
4. 未命中时通过 `apkFetcher` 从 APK upstream 拉取
5. 写回缓存并返回客户端

### APT 请求

典型路径示例：

- `http://deb.debian.org/debian/dists/bookworm/InRelease`
- `http://deb.debian.org/debian/pool/main/h/hello/hello_2.10-3_amd64.deb`
- `http://deb.debian.org/debian/dists/bookworm/main/binary-amd64/by-hash/SHA256/...`

处理流程：

1. `APTAdapter` 识别请求
2. 解析出真实目标 URL
3. 生成 `apt/<host>/<path>` 形式的缓存键
4. 校验已有缓存
5. 拉取上游内容
6. 如为索引文件，异步或同步更新 `APTIndexService`
7. 如启用了校验，则验证 `by-hash` 或 `.deb`

### 通用代理请求

典型场景：

- HTTPS APT 源经由 `CONNECT` 访问
- 非包类 HTTP 请求透传

处理流程：

1. `ProxyAdapter` 识别绝对 URL 或 `CONNECT`
2. 根据配置判断是否允许
3. HTTP 请求直接转发
4. HTTPS 请求通过隧道双向复制

## 这次重构刻意移除的内容

为了先把核心链路收紧，以下能力没有保留在新主干里：

- 管理后台
- 管理认证
- 旧版复杂 CLI 参数
- 细粒度缓存策略
- 缓存配额管理
- 自动清理任务
- 限流
- 旧的 i18n 主流程依赖

这不是功能价值判断，而是阶段性裁剪。目标是先让核心内核稳定、可测试、可扩展。

## 监控变化

监控仍然通过 [`utils/monitoring.go`](../utils/monitoring.go) 提供。

本次重构保留并补充了更贴近核心链路的指标：

- cache hit / miss
- upstream requests
- upstream failovers
- response bytes
- validation failures
- memory cache 统计

管理台不再是主要观察入口，Prometheus 指标和 `/_health` 成为新的最小运维面。

## 已有测试覆盖

当前新增或保留的测试主要覆盖：

- APK 缓存命中流程
- APT 缓存命中流程
- APT host 隔离缓存键
- `by-hash` 失败场景
- proxy 开关行为
- `CONNECT` 开关行为
- upstream failover

对应测试文件：

- [`cmd/apk-cache/pipeline_test.go`](../cmd/apk-cache/pipeline_test.go)
- [`internal/upstream/upstream_test.go`](../internal/upstream/upstream_test.go)

## 当前已知限制

这次重构已经把骨架切换成功，但还有一些明确的后续工作：

- README、DEV 文档仍然引用旧 CLI 和旧配置，需要同步更新。
- `build.sh` 仍然保留了旧的管理页压缩逻辑，没有完全跟随新架构简化。
- APT 目前通过目标 URL 直接访问上游，没有单独的 APT upstream 配置层。
- `proxy.require_auth` 目前只作为配置位保留，认证逻辑尚未重新接入新内核。
- 非包类流量虽然可以透传，但仍不建议把当前服务当成通用企业代理使用。
- 缓存清理、配额治理、结构化日志等能力后续需要按新架构方式重新接入。

## 后续建议

建议后续按下面顺序继续演进：

1. 更新 README / DEV / Docker 使用文档，彻底切到新配置模型
2. 简化 `build.sh`，移除管理台相关历史包袱
3. 为 APT 增加可选的上游策略配置
4. 重新接入最小认证能力，仅作用于 proxy adapter
5. 在 pipeline 之上重建清理、配额、结构化日志等外围能力

## 总结

这次重构的本质，不是简单删旧代码，而是把系统从“多个并行流程拼接”改成了“一个统一流水线 + 多个协议适配器”的结构。

这样做带来的直接收益是：

- 运行时结构更简单
- 协议差异被局部化
- 缓存逻辑只保留一份
- 上游访问边界更清晰
- APT/APK/代理三条链路终于可以在同一套框架下继续演进

这份文档描述的是当前代码已经落下的架构，而不是理想蓝图。后续如继续调整，优先保持 `App -> Pipeline -> Adapter -> Service` 这条主干不再被外围功能污染。
