# APK Cache

[English](README_EN.md) | 简体中文

一个用于缓存 Alpine Linux APK 包的代理服务器，支持 SOCKS5 代理和多语言界面。

## 功能特性

- 🚀 自动缓存 Alpine Linux APK 包
- 📦 缓存命中时直接从本地提供服务
- 🔄 缓存未命中时从上游服务器获取并保存
- 🌐 支持 SOCKS5 代理访问上游服务器
- 💾 可配置的缓存目录和监听地址
- ⏱️ APKINDEX.tar.gz 索引文件自动过期刷新
- 🔒 客户端断开连接不影响缓存文件保存
- ⚡ 同时写入缓存和客户端，提升响应速度
- 🔐 文件级锁管理，避免并发下载冲突
- 🌍 多语言支持（中文/英文），自动检测系统语言

## 快速开始

### 安装

```bash
git clone git@github.com:tursom/apk-cache.git
cd apk-cache
go build -o apk-cache cmd/apk-cache/main.go
```

### 运行

```bash
# 默认配置运行（自动检测系统语言）
./apk-cache

# 自定义配置
./apk-cache -addr :3142 -cache ./cache -proxy socks5://127.0.0.1:1080

# 指定语言
./apk-cache -locale zh  # 中文
./apk-cache -locale en  # 英文
```

### 命令行参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-addr` | `:3142` | 监听地址 |
| `-cache` | `./cache` | 缓存目录路径 |
| `-upstream` | `https://dl-cdn.alpinelinux.org` | 上游服务器地址 |
| `-proxy` | (空) | SOCKS5 代理地址，格式: `socks5://[username:password@]host:port` |
| `-index-cache` | `24h` | APKINDEX.tar.gz 索引文件缓存时间 |
| `-locale` | (自动检测) | 界面语言 (`en`/`zh`)，留空则根据 `LANG` 环境变量自动检测 |

## 使用方法

### 配置 Alpine Linux 使用缓存服务器

编辑 `/etc/apk/repositories`:

```bash
# 将默认的镜像地址替换为缓存服务器地址
sed -i 's/https:\/\/dl-cdn.alpinelinux.org/http:\/\/your-cache-server:3142/g' /etc/apk/repositories
```

或者直接使用命令行:

```bash
# 安装软件包时指定缓存服务器
apk add --repositories-file /dev/null --repository http://your-cache-server:3142/alpine/v3.22/main <package-name>
```

## 工作原理

1. 客户端请求软件包时，服务器首先检查本地缓存
2. 对于 `APKINDEX.tar.gz` 索引文件，检查缓存是否过期（默认 24 小时）
3. 如果缓存命中且未过期 (`X-Cache: HIT`)，直接从本地文件返回
4. 如果缓存未命中 (`X-Cache: MISS`)，从上游服务器下载
5. 下载时使用**文件级锁**避免多个请求重复下载同一文件
6. 同时写入缓存文件和客户端响应，提升用户体验
7. 即使客户端中途断开连接，缓存文件也会完整保存
8. 下载完成后，文件保存到本地缓存目录供后续请求使用

### 缓存策略

- **APK 包文件**: 永久缓存，不会过期
- **APKINDEX.tar.gz 索引文件**: 定期过期（默认 24 小时），可通过 `-index-cache` 参数调整
- **其他文件**: 永久缓存

### 并发控制

- 使用文件级锁管理器，避免并发请求重复下载同一文件
- 第一个请求获取锁并下载文件，后续请求等待锁释放后直接读取缓存
- 引用计数机制自动清理不再使用的锁，避免内存泄漏

## 注意事项

- 缓存目录会随着使用逐渐增大，建议定期清理或设置磁盘配额
- 使用 SOCKS5 代理时，确保代理服务器可访问
- 服务器默认监听所有网络接口，生产环境建议配置防火墙规则
- APKINDEX.tar.gz 索引文件会定期刷新以获取最新的软件包信息
- 索引缓存时间建议设置在 1 小时到 24 小时之间，生产环境可根据实际情况调整
- 并发请求同一文件时，只会下载一次，其他请求会等待并共享缓存

## 高级配置

### 多语言支持

程序会自动检测系统语言（通过 `LC_ALL`、`LC_MESSAGES` 或 `LANG` 环境变量）：

```bash
# 使用系统默认语言
./apk-cache

# 强制使用中文
./apk-cache -locale zh
LANG=zh_CN.UTF-8 ./apk-cache

# 强制使用英文
./apk-cache -locale en
LANG=en_US.UTF-8 ./apk-cache
```

### 调整索引缓存时间

```bash
# stable 版本（生产环境推荐）- 主要是安全更新，更新不频繁
./apk-cache -index-cache 24h   # 1 天

# edge 版本（开发环境）- 包更新频繁
./apk-cache -index-cache 2h    # 2 小时

# 对时效性要求极高的场景
./apk-cache -index-cache 1h    # 1 小时

# 内网环境，对上游服务器负载不敏感
./apk-cache -index-cache 12h   # 12 小时
```

**注意**: Go 的 `time.ParseDuration` 不支持 `d`（天）单位，请使用小时 `h`。例如 1 天 = `24h`，7 天 = `168h`。

### 使用带认证的 SOCKS5 代理

```bash
./apk-cache -proxy socks5://username:password@127.0.0.1:1080
```

### 自定义上游服务器

```bash
# 使用清华大学镜像
./apk-cache -upstream https://mirrors.tuna.tsinghua.edu.cn/alpine

# 使用阿里云镜像
./apk-cache -upstream https://mirrors.aliyun.com/alpine
```

## 性能特性

### 并发安全

- **文件级锁管理**: 使用自定义的 `FileLockManager`，确保同一文件只会被下载一次
- **引用计数**: 自动管理锁的生命周期，避免内存泄漏
- **双重检查**: 获取锁后再次检查缓存，避免重复下载

### 客户端友好

- **流式传输**: 边下载边传输给客户端，无需等待完整下载
- **断点续传**: 客户端断开不影响缓存完整性
- **并发下载**: 不同文件可以并发下载，互不影响

## 项目结构

```
apk-cache/
├── cmd/
│   └── apk-cache/
│       ├── main.go          # 主程序
│       ├── lockman.go       # 文件锁管理器
│       └── lockman_test.go  # 单元测试
├── locales/
│   ├── en.toml             # 英文翻译
│   └── zh.toml             # 中文翻译
├── cache/                  # 缓存目录（运行时生成）
├── go.mod
├── go.sum
└── README.md
```

## 依赖

- `golang.org/x/net/proxy` - SOCKS5 代理支持
- `github.com/nicksnyder/go-i18n/v2` - 国际化支持
- `github.com/BurntSushi/toml` - TOML 配置文件解析

## 许可证

GPLv3 License

## 贡献

欢迎提交 Issue 和 Pull Request！
