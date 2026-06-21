# Hash 持久化存储设计文档

## 1. 背景

当前 APK/APT 的 hash 索引在进程内存中，来源是缓存目录中的 `APKINDEX.tar.gz`、`Release`、`Packages*`、`Sources*`。服务重启后需要重新扫描和解析这些索引文件；本地缓存文件的实际 hash 也可能在校验时重复计算。

这些操作会消耗 CPU 和磁盘 IO，尤其是：

- APT `Packages*` / `Sources*` 解析量较大。
- `.apk` / `.deb` 文件实际 hash 计算需要读取完整文件。
- by-hash 索引需要把 URL hash 反查回原始索引路径。

因此需要新增持久化 hash store。设计上使用 Pebble 这类本地 KV 数据库，而不是把 hash 热路径放到 SQLite。

## 2. 设计结论

- hash 热路径使用 Pebble，不使用 SQLite。
- SQLite 只保存管理台配置、日志、分页搜索摘要等关系型数据。
- Pebble key 必须二进制化，不使用字符串拼接 key。
- 可变长字段必须放在 key 的最后面。
- hash 算法并入 key namespace，不在 expected/actual 热路径 key 中额外保存 `algo` 字段。
- 缓存文件 path 不直接进入热路径 key，先映射为固定 16 字节 `path_id`。
- Pebble 同时保存两类 hash：
  - 索引声明的期望 hash。
  - 本地缓存文件实际 hash 的计算结果。
- 本地文件实际 hash 使用 `size + mtime_unix_nano` 判断是否可以复用，避免每次请求读取完整文件。
- APT by-hash 需要单独保存 `host + hash kind + hash -> 原始索引 path` 的映射。
- Pebble 缺失或版本不兼容时，允许从磁盘索引文件重建。

## 3. 术语

| 术语 | 含义 |
| --- | --- |
| 期望 hash | 上游索引声明的 hash，例如 APKINDEX checksum、APT Release/Packages 中的 SHA256 |
| 实际 hash | 本地缓存文件内容重新计算得到的 hash |
| source index | 写入 hash records 的来源索引文件，例如 APKINDEX、Release、Packages |
| target file | 被校验的目标缓存文件，例如 `.apk`、`.deb`、`Packages.xz` |
| cache key | 相对 `cache.root` 的规范缓存路径 |
| path id | cache key 经过 SHA256 截断得到的 16 字节固定标识 |

## 4. 存储分工

| 存储 | 职责 |
| --- | --- |
| SQLite | 管理台配置、管理员、session、上游、请求日志、可分页查询的管理元数据 |
| Pebble | hash expected records、actual hash cache、source mapping、APT by-hash mapping、路径字典 |

原则：

- hash 校验热路径只做 KV 点查。
- 管理页面需要分页/过滤的数据仍放 SQLite。
- Pebble 中不使用 JSON key。
- Pebble key 使用二进制编码。
- 可变长字段必须放在 key 的最后。
- hash 字段不是任意可变长字段；它的长度由 key namespace 对应的算法决定。

## 5. 数据分类

### 5.1 期望 hash

索引声明的期望 hash：

- APK：`APKINDEX.tar.gz` 中 `.apk` 包的 checksum。
- APT：`Release` / `InRelease` / `Packages*` / `Sources*` 中声明的 SHA256。

### 5.2 实际 hash 缓存

本地缓存文件实际计算出的 hash：

- 文件内容 hash。
- 文件 size。
- 文件 mtime。
- 计算时间。

只有 size 或 mtime 变化时，才重新读取文件计算 hash。

### 5.3 来源映射

索引文件到目标文件的映射：

- 用于索引更新时删除旧 records。
- 用于管理台展示某条 expected hash 来自哪个索引文件。

### 5.4 APT by-hash 映射

APT `by-hash/<algorithm>/<hash>` 到原始索引路径的映射：

- Release 记录 `main/binary-amd64/Packages.xz` 的 SHA256。
- 客户端通过 `by-hash/SHA256/<hash>` 下载时，需要根据 hash 找回原始 `Packages.xz` 路径。
- 找到原始路径后，才能按 `Packages.xz` 的压缩格式解析包记录。

## 6. 路径标识

热路径 key 不直接使用可变长 path，而是使用固定长度 `path_id`。

### 6.1 path 规范化

`cache_key` 定义为相对 `cache.root` 的规范路径：

```text
alpine/v3.23/main/x86_64/APKINDEX.tar.gz
apt/deb.debian.org/debian/dists/bookworm/Release
apt/deb.debian.org/debian/pool/main/h/hello/hello_1_amd64.deb
```

规范化规则：

- 使用 `/` 作为分隔符。
- 不包含前导 `/`。
- 不包含 `..`。
- 不包含 NUL。
- host 使用当前缓存路径中的 sanitize 结果。

### 6.2 path_id

`path_id` 为 16 字节：

```text
path_id = sha256(cache_key)[0:16]
```

选择 SHA256 截断而不是额外引入 xxhash，是为了避免新增 hash 依赖；path 本身较短，计算成本可以忽略。

写入 path dictionary 时必须做碰撞检查：

- 如果 `path_id` 不存在，写入。
- 如果 `path_id` 已存在且 value 等于当前 path，复用。
- 如果 `path_id` 已存在但 value 不同，返回 collision 错误，拒绝写入。

## 7. 二进制 key 编码

所有 key 都以 namespace 和 version 开头：

```text
ns:u8 | version:u8 | fields...
```

当前 version 固定为 `0x01`。

数值字段使用 big-endian 固定宽度编码，保证 Pebble 的字节序排序和 prefix scan 行为稳定。

算法化 namespace：

| namespace | 含义 | hash 长度 |
| --- | --- | --- |
| `0x21` | expected_sha1 | 20 bytes |
| `0x22` | expected_sha256 | 32 bytes |
| `0x23` | actual_sha1 | 20 bytes |
| `0x24` | actual_sha256 | 32 bytes |
| `0x25` | expected_by_hash_sha1 | 20 bytes |
| `0x26` | expected_by_hash_sha256 | 32 bytes |
| `0x27` | apt_byhash_sha1 | 20 bytes |
| `0x28` | apt_byhash_sha256 | 32 bytes |

其他 namespace：

| namespace | 含义 |
| --- | --- |
| `0x03` | source mapping |
| `0x10` | dictionary by id |
| `0x11` | dictionary by value |

约束：

- 已发布的 namespace 不能改变含义。
- 新增算法时必须新增对应 namespace，并定义固定 digest 长度。
- key decoder 先读取 namespace，再决定后续 hash 字段长度。
- 因为 hash 长度由 namespace 确定，所以 hash 后面可以继续放其他固定长度字段。
- expected/actual 热路径 key 不再带 `algo:u8`。

source mapping 需要覆盖同一个 source 下的所有算法记录，因此保留一个 `hash_kind:u8` 字段：

| 值 | 算法 | 长度 |
| --- | --- | --- |
| `0x01` | sha1 | 20 bytes |
| `0x02` | sha256 | 32 bytes |

字典 kind 枚举：

| 值 | 类型 |
| --- | --- |
| `0x01` | cache path |
| `0x02` | host |

record type 枚举：

| 值 | 类型 |
| --- | --- |
| `0x01` | apk package |
| `0x02` | apt release file |
| `0x03` | apt package file |
| `0x04` | apt source file |

### 7.1 expected key

```text
expected_sha1:
  0x21 | version:u8 | target_path_id:16

expected_sha256:
  0x22 | version:u8 | target_path_id:16
```

长度固定 18 字节。

用途：

- 校验缓存文件时，通过目标 path 和算法对应 namespace 点查 expected hash。

### 7.2 actual key

```text
actual_sha1:
  0x23 | version:u8 | cache_path_id:16

actual_sha256:
  0x24 | version:u8 | cache_path_id:16
```

长度固定 18 字节。

用途：

- 校验缓存文件时，通过目标 path 和算法对应 namespace 点查实际 hash 缓存。

### 7.3 expected by hash key

```text
expected_by_hash_sha1:
  0x25 | version:u8 | expected_hash:20 | target_path_id:16

expected_by_hash_sha256:
  0x26 | version:u8 | expected_hash:32 | target_path_id:16
```

长度由算法决定：

- sha1: 38 字节。
- sha256: 50 字节。

用途：

- 支持管理台或诊断命令按 hash 反查缓存文件。
- 支持未来需要从 hash 定位 target 的场景。

因为 `expected_hash` 长度由 namespace 决定，所以 `target_path_id` 可以放在 hash 后面；这不违反“任意可变长字段必须放最后”的规则。

### 7.4 source mapping key

```text
0x03 | version:u8 | source_index_path_id:16 | target_path_id:16 | hash_kind:u8
```

长度固定 35 字节。

用途：

- 通过 `0x03 | version | source_index_path_id` prefix 找到某个索引旧写入的所有 target。
- 索引更新时先按 prefix scan 删除旧 expected，再写入新 expected。

### 7.5 APT by-hash key

```text
apt_byhash_sha1:
  0x27 | version:u8 | host_id:16 | expected_hash:20

apt_byhash_sha256:
  0x28 | version:u8 | host_id:16 | expected_hash:32
```

`expected_hash` 长度由 namespace 决定；当前 key 仍把 hash 放在最后，便于按 `namespace + version + host_id` 做 prefix scan。

用途：

- 根据 host、算法和 URL 中的 hash 找回原始索引 path。
- value 为原始索引 `path_id`。

### 7.6 dictionary by id key

```text
0x10 | version:u8 | kind:u8 | id:16
```

长度固定 19 字节。

用途：

- `path_id -> cache_key`
- `host_id -> host`

value 为 UTF-8 字符串 bytes。

### 7.7 dictionary by value key

```text
0x11 | version:u8 | kind:u8 | value:bytes
```

`value` 是可变长字段，因此放在 key 最后。

用途：

- 可选反查：`cache_key -> path_id` 或 `host -> host_id`。
- 管理台如果需要按 path 前缀扫描，可以使用这个 namespace。

value 为 16 字节 id。

注意：该 key 后面不能再追加字段；任何需要追加字段的查询都必须使用固定长度 id key。

## 8. value 编码

value 使用紧凑二进制编码，不使用 JSON。

### 8.1 expected value

```text
record_type:u8
expected_size:uvarint
expected_hash:bytes(hash_len_by_namespace)
source_index_path_id:16
updated_unix_nano:varint
```

说明：

- `expected_size=0` 表示索引没有提供 size。
- `source_index_path_id` 用于追踪来源。
- hash 长度由 key namespace 决定。

### 8.2 actual value

```text
size_bytes:uvarint
mtime_unix_nano:varint
actual_hash:bytes(hash_len_by_namespace)
computed_unix_nano:varint
```

命中条件：

- 当前文件 size 等于 `size_bytes`。
- 当前文件 mtime unix nano 等于 `mtime_unix_nano`。

两个条件都满足时，复用 `actual_hash`，不重新读取文件。

### 8.3 source mapping value

```text
record_type:u8
expected_hash:bytes(hash_len_by_hash_kind)
```

source mapping 的主要定位信息在 key 中，value 额外保留 expected hash，便于索引更新时删除对应的 expected by hash key，而不需要再读 expected value。

source mapping value 中的 hash 长度由 key 末尾的 `hash_kind` 决定。

### 8.4 APT by-hash value

```text
original_index_path_id:16
```

## 9. 写入流程

### 9.1 加载 APKINDEX

1. 规范化 `APKINDEX.tar.gz` cache path，生成 `source_index_path_id`。
2. 解析 APKINDEX。
3. 对每个包生成目标 `.apk` path 和 `target_path_id`。
4. 写入 dictionary。
5. 删除该 source 旧 records。
6. 批量写入：
   - expected key/value。
   - expected by hash key。
   - source mapping key/value。

### 9.2 加载 APT Release/InRelease

1. 规范化 Release cache path，生成 `source_index_path_id`。
2. 解析 SHA256 文件清单。
3. 对每个 `Packages*` / `Sources*` / 其他 Release 引用文件生成 `target_path_id`。
4. 写入 dictionary。
5. 删除该 source 旧 records。
6. 批量写入：
   - expected key/value。
   - expected by hash key。
   - source mapping key/value。
   - APT by-hash key/value。

APT by-hash key 的 `expected_hash` 来自 Release 中的 SHA256。

### 9.3 加载 APT Packages/Sources

1. 规范化 Packages/Sources cache path，生成 `source_index_path_id`。
2. 如果文件来自 by-hash 请求，先通过 APT by-hash mapping 找到原始索引 path。
3. 按原始索引 path 的文件名选择解压方式。
4. 解析包记录。
5. 删除该 source 旧 records。
6. 批量写入 expected、expected by hash 和 source mapping。

## 10. 索引更新删除旧记录

更新某个 source index 时不能只覆盖新记录，否则旧包记录会残留。

流程：

1. 构造 source prefix：

```text
0x03 | version | source_index_path_id
```

2. prefix scan 找到旧 target、hash kind 和 source mapping value 中的 expected hash。
3. 对每个旧 target 删除 expected key。
4. 对每个旧 target 删除 expected by hash key。
5. 删除所有旧 source mapping key。
6. 写入新 expected、expected by hash 和 source mapping。
7. 提交 Pebble batch。

如果某个 target 同时来自多个 source，后续需要引入 source priority 或 refcount。首版按当前缓存模型，默认一个目标 path 只保留最新 source 的 expected record。

## 11. 校验流程

### 11.1 expected 查找

输入：

- `cache_key`
- `hash_kind`

流程：

1. 规范化 cache path。
2. 计算 `path_id`。
3. 通过 `hash_kind` 选择 expected namespace，点查 expected key。
4. 未找到时返回 `ErrIndexUnavailable` 或按当前协议逻辑跳过。

### 11.2 actual 查找

输入：

- `cache_key`
- `hash_kind`
- 当前文件 stat。

流程：

1. 通过 `hash_kind` 选择 actual namespace，点查 actual key。
2. 如果不存在，重新计算文件 hash，写入 actual。
3. 如果存在但 size 或 mtime 不匹配，重新计算文件 hash，覆盖 actual。
4. 如果存在且 size/mtime 匹配，直接返回 actual hash。

### 11.3 对比

对比 expected 和 actual：

- hash 相同：校验通过。
- size 不同：校验失败。
- hash 不同：校验失败。

校验失败时：

- 删除缓存文件。
- 删除 actual key。
- expected key 保留，因为索引声明仍然有效。

## 12. 缓存文件删除

删除缓存文件时：

- 删除磁盘文件。
- 删除 actual key。
- SQLite `cache_objects` 标记 deleted 或删除。
- 不删除 expected key，除非对应 source index 被删除或更新。

删除索引文件时：

- 删除索引文件本身的 actual key。
- 按 source mapping 删除该索引写入的 expected records。
- 按 source mapping 删除该索引写入的 expected by hash records。
- 删除 source mapping。

## 13. 启动恢复

启动流程：

1. 打开 Pebble。
2. 打开 SQLite。
3. 从 Pebble expected prefix 加载内存 hash records。
4. 如果 Pebble 为空或版本不匹配：
   - 扫描磁盘缓存中的索引文件。
   - 重新解析并写入 Pebble。
   - 再加载内存 records。
5. 如果 Pebble 损坏：
   - 关闭服务并提示修复，或在配置允许时删除 hash store 后重建。

建议配置：

```text
hash_store.path = "${data_root}/hash.pebble"
hash_store.rebuild_on_corruption = false
```

## 14. stat 缓存的安全边界

actual hash cache 默认使用 `size + mtime_unix_nano` 判断是否可复用。

优点：

- 避免每次请求读取完整 `.apk` / `.deb`。
- 对正常缓存写入、删除、重新下载足够可靠。

边界：

- 如果有人恶意篡改文件，并刻意恢复原始 size 和 mtime，则可能复用旧 actual hash。

建议配置：

```text
hash_store.trust_file_stat = true
hash_store.actual_revalidate_interval = "24h"
```

当 `actual_revalidate_interval > 0` 时，即使 size/mtime 一致，超过间隔也后台重算 actual hash。

## 15. 管理台展示

管理台不直接分页扫描 Pebble 热路径 key。

推荐做法：

- SQLite 保存缓存对象和索引摘要，用于列表和搜索。
- 详情页需要 hash 时，通过 hash service 点查 Pebble。
- 诊断页面展示 Pebble 状态：
  - path。
  - estimated size。
  - expected record count。
  - actual record count。
  - last rebuild time。
  - corruption/rebuild 状态。

## 16. 测试计划

### 16.1 key 编码测试

- expected key 长度固定为 18。
- actual key 长度固定为 18。
- expected by hash key 长度由 namespace 决定：sha1 为 38，sha256 为 50。
- source mapping key 长度固定为 35。
- hash 字段长度由 namespace 或 hash kind 决定，hash 后面可以继续放固定长度字段。
- dictionary by value key 的 variable value 在末尾。
- prefix upper bound 可以正确覆盖同 namespace/version/source。

### 16.2 dictionary 测试

- 同 path 生成同 path_id。
- path_id 已存在且 path 相同可复用。
- path_id 已存在但 path 不同返回 collision。

### 16.3 写入与删除测试

- 加载 APKINDEX 写入 expected/source records。
- 更新 APKINDEX 删除旧 records。
- 加载 Release 写入 APT by-hash mapping。
- 通过 by-hash 下载 Packages 后能按原始索引路径解析。

### 16.4 actual hash cache 测试

- 首次校验计算 hash 并写入 Pebble。
- size/mtime 不变时不重新计算。
- size 变化时重新计算。
- mtime 变化时重新计算。
- 校验失败删除 actual key。

### 16.5 集成测试

- 当前真实 APK/APT fixture 测试必须继续通过。
- 重启后不扫描索引文件也能从 Pebble 加载 expected records。
- 删除 Pebble 后可以从磁盘索引重建。

## 17. 实施阶段

### Phase 1: Pebble hash store 基础

- 引入 Pebble。
- 实现 binary key encoder/decoder。
- 实现 dictionary。
- 实现 expected/actual/source/by-hash repository。

### Phase 2: APK/APT 索引写入 Pebble

- APKINDEX 加载时写入 Pebble。
- APT Release/Packages 加载时写入 Pebble。
- 启动时从 Pebble 恢复内存 records。

### Phase 3: actual hash cache

- APK/APT 校验使用 actual hash cache。
- 缓存删除时清理 actual。
- 加入 revalidate interval。

### Phase 4: 管理台接入

- 管理台展示 hash store 状态。
- cache detail 页面点查 expected/actual。
- 诊断包包含 hash store 摘要。
