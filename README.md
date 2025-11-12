# APK Cache
一个用于缓存 Alpine Linux APK 包的代理服务器,支持 SOCKS5 代理。

## 功能特性

- 🚀 自动缓存 Alpine Linux APK 包
- 📦 缓存命中时直接从本地提供服务
- 🔄 缓存未命中时从上游服务器获取并保存
- 🌐 支持 SOCKS5 代理访问上游服务器
- 💾 可配置的缓存目录和监听地址
- ⏱️ APKINDEX.tar.gz 索引文件自动过期刷新
- 🔒 客户端断开连接不影响缓存文件保存
- 🔄 同时写入缓存和客户端，提升响应速度

## 快速开始

### 安装

```bash
git clone git@github.com:tursom/apk-cache.git
cd apk-cache
go build -o apk-cache cmd/apk-cache/main.go
```

### 运行

```bash
# 默认配置运行
./apk-cache

# 自定义配置
./apk-cache -addr :8080 -cache ./cache -proxy socks5://127.0.0.1:1080
```

### 命令行参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-addr` | `:8080` | 监听地址 |
| `-cache` | `./cache` | 缓存目录路径 |
| `-upstream` | `https://dl-cdn.alpinelinux.org` | 上游服务器地址 |
| `-proxy` | (空) | SOCKS5 代理地址,格式: `socks5://[username:password@]host:port` |
| `-index-cache` | `1h` | APKINDEX.tar.gz 索引文件缓存时间 |

## 使用方法

### 配置 Alpine Linux 使用缓存服务器

编辑 `/etc/apk/repositories`:

```bash
# 将默认的镜像地址替换为缓存服务器地址
sed -i 's/https:\/\/dl-cdn.alpinelinux.org/http:\/\/your-cache-server:8080/g' /etc/apk/repositories
```

或者直接使用命令行:

```bash
# 安装软件包时指定缓存服务器
apk add --repositories-file /dev/null --repository http://your-cache-server:8080/alpine/v3.22/main <package-name>
```

## 工作原理

1. 客户端请求软件包时,服务器首先检查本地缓存
2. 对于 `APKINDEX.tar.gz` 索引文件，检查缓存是否过期（默认 1 小时）
3. 如果缓存命中且未过期(`X-Cache: HIT`),直接从本地文件返回
4. 如果缓存未命中(`X-Cache: MISS`),从上游服务器(dl-cdn.alpinelinux.org)下载
5. 下载时同时写入缓存文件和客户端响应，提升用户体验
6. 即使客户端中途断开连接，缓存文件也会完整保存
7. 下载的文件会被保存到本地缓存目录,供后续请求使用

### 缓存策略

- **APK 包文件**: 永久缓存，不会过期
- **APKINDEX.tar.gz 索引文件**: 定期过期（默认 1 小时），可通过 `-index-cache` 参数调整
- **其他文件**: 永久缓存

## 注意事项

- 缓存目录会随着使用逐渐增大,建议定期清理或设置磁盘配额
- 使用 SOCKS5 代理时,确保代理服务器可访问
- 服务器默认监听所有网络接口,生产环境建议配置防火墙规则
- APKINDEX.tar.gz 索引文件会定期刷新以获取最新的软件包信息
- 索引缓存时间建议设置在 30 分钟到 2 小时之间，生产环境可根据实际情况调整

## 高级配置

### 调整索引缓存时间

```bash
# 设置索引文件缓存 30 分钟
./apk-cache -index-cache 30m

# 设置索引文件缓存 2 小时
./apk-cache -index-cache 2h
```

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
