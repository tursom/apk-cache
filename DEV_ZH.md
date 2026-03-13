# APK Cache 开发指南

## 项目概述

APK Cache 是一个高性能的代理服务器，用于缓存 Alpine Linux APK 包和 Debian/Ubuntu APT 包。它采用三级缓存架构（内存 → 文件 → 上游），具备健康监控和自愈能力，以及全面的安全功能。

## 项目结构

```
apk-cache/
├── cmd/
│   ├── apk-cache/           # 主应用程序
│   │   ├── main.go          # 程序入口
│   │   ├── config.go        # 配置加载
│   │   ├── cache.go         # 文件缓存实现
│   │   ├── memory_cache.go  # 内存缓存层
│   │   ├── handlers.go      # HTTP 请求处理
│   │   ├── admin.go         # 管理仪表板
│   │   ├── upstream.go      # 上游服务器管理
│   │   ├── cleanup.go       # 缓存清理逻辑
│   │   ├── cache_quota.go   # 缓存配额管理
│   │   ├── cache_apt.go     # APT 代理支持
│   │   ├── http_proxy.go    # HTTP 代理支持
│   │   └── access_tracker.go # 访问跟踪
│   └── apt-hash/            # APT 哈希工具
├── build.sh                 # 构建脚本（必需）
├── Dockerfile               # Docker 构建文件
├── go.mod                   # Go 模块定义
├── go.sum                   # Go 依赖
├── config.example.toml      # 配置示例
└── cmd/apk-cache/admin.html # 管理界面 HTML
```

## 前置要求

- Go 1.25 或更高版本
- Git
- HTML 压缩工具（可选）: html-minifier, python-htmlmin 或 esbuild

## 构建说明

**重要**：始终使用构建脚本，不要直接使用 `go build`：

```bash
./build.sh
```

构建脚本自动执行以下操作：
1. 检测系统中可用的 HTML 压缩工具
2. 压缩管理界面 HTML
3. 创建 gzip 压缩版本
4. 使用优化选项构建 Go 应用程序

## 运行应用程序

```bash
# 默认配置
./apk-cache

# 使用配置文件
./apk-cache -config config.toml

# 使用命令行参数
./apk-cache -addr :3142 -cache ./cache -proxy socks5://127.0.0.1:1080
```

## 运行测试

### 前置要求

- 已安装并运行 Docker

### 使用 run_test.sh

项目包含一个集成测试脚本，用于测试 APK 和 APT 缓存功能：

```bash
./run_test.sh
```

测试脚本执行以下步骤：
1. 使用 `build.sh` 构建应用程序（在 Docker 构建过程中自动调用）
2. 构建 Docker 镜像
3. 启动 apk-cache 服务
4. 使用 Alpine Linux 客户端测试（apk update）
5. 使用 Debian 客户端测试（apt-get update）

脚本会在测试完成后自动清理测试环境（容器、镜像），但保留缓存目录（`/tmp/apk-cache-test-cache`）以便检查。

### 支持的参数

| 参数 | 说明 |
|------|------|
| `--goproxy <value>` | 设置 Go 构建依赖的 GOPROXY |
| `--alpine-apk-mirror <url>` | Docker 构建时使用的 Alpine 镜像（如 http://mirror/alpine） |
| `--apk-mirror <url>` | --alpine-apk-mirror 的别名 |
| `--lang <zh\|en>` | 设置语言（默认：自动检测） |
| `-h, --help` | 显示帮助信息 |

### 示例

```bash
# 使用中文输出运行测试
./run_test.sh --lang zh

# 使用自定义 Go 代理运行测试
./run_test.sh --goproxy https://goproxy.cn

# 使用自定义 Alpine 镜像运行测试
./run_test.sh --alpine-apk-mirror http://mirror.example.com/alpine
```

## 代码规范

### 命名
- 变量和函数名使用 camelCase
- 导出的类型、函数和常量使用 PascalCase
- 未导出的全局变量使用混合大小写（mixedCaps）

### 错误处理
- 始终显式处理错误
- 适当情况下返回错误而不是静默记录
- 使用 `fmt.Errorf("上下文: %v", err)` 创建错误消息
- 使用 `errors.New()` 创建静态错误消息

### 日志
- 使用适当级别的结构化日志
- 在日志消息中包含上下文

### 国际化 (i18n)
- 所有面向用户的字符串必须通过 `i18n.T()` 使用国际化系统
- 永远不要硬编码可见字符串 - 使用翻译键
- 在 `utils/i18n/` 目录中提供翻译键

### 配置
- 所有配置选项必须有 CLI 标志
- 配置也可以从 TOML 文件加载
- 使用合理的默认值并提供清晰的文档

## 关键组件

### 缓存架构（三级）
1. **内存缓存**：带 TTL 支持的 LRU 缓存，最快访问
2. **文件缓存**：持久化磁盘存储
3. **上游**：原始包来源

### 健康检查系统
- 定期检查上游服务器、文件系统、内存缓存和缓存配额
- 自动故障转移到健康的上游服务器
- 常见问题的自愈机制

### 安全功能
- 代理身份验证（SOCKS5/HTTP）
- 管理界面身份验证
- IP 白名单
- 反向代理支持
- 路径安全验证

## 添加新功能

1. 创建新分支：`git checkout -b feature/your-feature`
2. 按照代码规范进行更改
3. 为新功能添加测试
4. 更新文档
5. 提交拉取请求

## 依赖项

核心依赖（见 `go.mod`）：
- `github.com/prometheus/client_golang` - Prometheus 指标
- `go.etcd.io/bbolt` - 嵌入式数据库
- `golang.org/x/net` - HTTP 工具
- `github.com/nicksnyder/go-i18n/v2` - 国际化
