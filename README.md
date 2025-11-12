# APK Cache
一个用于缓存 Alpine Linux APK 包的代理服务器,支持 SOCKS5 代理。

## 功能特性

- 🚀 自动缓存 Alpine Linux 软件包
- 📦 缓存命中时直接从本地提供服务
- 🔄 缓存未命中时从上游服务器获取并保存
- 🌐 支持 SOCKS5 代理访问上游服务器
- 💾 可配置的缓存目录和监听地址

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
2. 如果缓存命中(`X-Cache: HIT`),直接从本地文件返回
3. 如果缓存未命中(`X-Cache: MISS`),从上游服务器(dl-cdn.alpinelinux.org)下载
4. 下载的文件会被保存到本地缓存目录,供后续请求使用

## 注意事项

- 缓存目录会随着使用逐渐增大,建议定期清理或设置磁盘配额
- 使用 SOCKS5 代理时,确保代理服务器可访问
- 服务器默认监听所有网络接口,生产环境建议配置防火墙规则
