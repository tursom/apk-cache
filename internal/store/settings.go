package store

import (
	"encoding/json"
	"fmt"

	"github.com/tursom/apk-cache/internal/config"
)

type settingDef struct {
	key             string
	valueType       string
	restartRequired bool
	marshal         func(*config.Config) (json.RawMessage, error)
	apply           func(*config.Config, json.RawMessage) error
}

type settingMeta struct {
	Group       string
	Title       string
	Description string
	Control     string
	Editable    bool
	Sensitive   bool
}

type SettingSchema struct {
	Key             string `json:"key"`
	Group           string `json:"group"`
	Title           string `json:"title"`
	Description     string `json:"description"`
	ValueType       string `json:"value_type"`
	Control         string `json:"control"`
	Editable        bool   `json:"editable"`
	HotReload       bool   `json:"hot_reload"`
	RestartRequired bool   `json:"restart_required"`
	Sensitive       bool   `json:"sensitive"`
}

var settingDefs = []settingDef{
	stringSetting("server.listen", true, func(c *config.Config) *string { return &c.Server.Listen }),
	stringSetting("database.path", true, func(c *config.Config) *string { return &c.Database.Path }),
	stringSetting("cache.root", true, func(c *config.Config) *string { return &c.Cache.Root }),
	stringSetting("cache.data_root", true, func(c *config.Config) *string { return &c.Cache.DataRoot }),
	stringSetting("cache.index_ttl", false, func(c *config.Config) *string { return &c.Cache.IndexTTL }),
	stringSetting("cache.package_ttl", false, func(c *config.Config) *string { return &c.Cache.PackageTTL }),
	boolSetting("cache.memory.enabled", false, func(c *config.Config) *bool { return &c.Cache.Memory.Enabled }),
	stringSetting("cache.memory.max_size", false, func(c *config.Config) *string { return &c.Cache.Memory.MaxSize }),
	stringSetting("cache.memory.max_item_size", false, func(c *config.Config) *string { return &c.Cache.Memory.MaxItemSize }),
	stringSetting("cache.memory.ttl", false, func(c *config.Config) *string { return &c.Cache.Memory.TTL }),
	intSetting("cache.memory.max_items", false, func(c *config.Config) *int { return &c.Cache.Memory.MaxItems }),
	stringSetting("transport.timeout", false, func(c *config.Config) *string { return &c.Transport.Timeout }),
	stringSetting("transport.idle_conn_timeout", false, func(c *config.Config) *string { return &c.Transport.IdleConnTimeout }),
	intSetting("transport.max_idle_conns", false, func(c *config.Config) *int { return &c.Transport.MaxIdleConns }),
	boolSetting("apk.enabled", false, func(c *config.Config) *bool { return &c.APK.Enabled }),
	boolSetting("apk.verify_hash", false, func(c *config.Config) *bool { return &c.APK.VerifyHash }),
	boolSetting("apk.verify_signature", false, func(c *config.Config) *bool { return &c.APK.VerifySignature }),
	stringSetting("apk.keys_dir", false, func(c *config.Config) *string { return &c.APK.KeysDir }),
	boolSetting("apt.enabled", false, func(c *config.Config) *bool { return &c.APT.Enabled }),
	boolSetting("apt.verify_hash", false, func(c *config.Config) *bool { return &c.APT.VerifyHash }),
	boolSetting("apt.load_index_async", false, func(c *config.Config) *bool { return &c.APT.LoadIndexAsync }),
	boolSetting("proxy.enabled", false, func(c *config.Config) *bool { return &c.Proxy.Enabled }),
	boolSetting("proxy.allow_connect", false, func(c *config.Config) *bool { return &c.Proxy.AllowConnect }),
	boolSetting("proxy.cache_non_package_requests", false, func(c *config.Config) *bool { return &c.Proxy.CacheNonPackage }),
	stringSetting("proxy.upstream_proxy", false, func(c *config.Config) *string { return &c.Proxy.UpstreamProxy }),
	stringSliceSetting("proxy.allowed_hosts", false, func(c *config.Config) *[]string { return &c.Proxy.AllowedHosts }),
	stringSetting("hash_store.path", true, func(c *config.Config) *string { return &c.HashStore.Path }),
	boolSetting("hash_store.rebuild_on_corruption", true, func(c *config.Config) *bool { return &c.HashStore.RebuildOnCorruption }),
	boolSetting("hash_store.trust_file_stat", false, func(c *config.Config) *bool { return &c.HashStore.TrustFileStat }),
	stringSetting("hash_store.actual_revalidate_interval", false, func(c *config.Config) *string { return &c.HashStore.ActualRevalidateInterval }),
}

var settingMetas = map[string]settingMeta{
	"server.listen":                         {Group: "runtime", Title: "HTTP 监听地址", Description: "Go 服务监听地址，修改后需重启进程。", Control: "text", Editable: true},
	"database.path":                         {Group: "runtime", Title: "SQLite 数据库路径", Description: "用于打开 SQLite 的启动配置，只能展示。", Control: "path", Editable: false},
	"cache.root":                            {Group: "cache", Title: "磁盘缓存目录", Description: "保存 APK/APT/proxy 缓存文件，保存后重启生效，不自动迁移旧缓存。", Control: "path", Editable: true},
	"cache.data_root":                       {Group: "cache", Title: "数据根目录", Description: "默认数据库和 Hash Store 根目录依赖它，首版只能展示。", Control: "path", Editable: false},
	"cache.index_ttl":                       {Group: "cache", Title: "索引 TTL", Description: "APKINDEX、APT Release/Packages 等索引缓存有效期。", Control: "duration", Editable: true},
	"cache.package_ttl":                     {Group: "cache", Title: "包文件 TTL", Description: "APK、deb 等包文件缓存有效期。", Control: "duration", Editable: true},
	"cache.memory.enabled":                  {Group: "memory", Title: "启用内存缓存", Description: "是否为小对象启用进程内缓存。", Control: "toggle", Editable: true},
	"cache.memory.max_size":                 {Group: "memory", Title: "内存缓存上限", Description: "进程内缓存总大小。", Control: "size", Editable: true},
	"cache.memory.max_item_size":            {Group: "memory", Title: "单对象内存缓存上限", Description: "超过该大小的对象不会放入内存缓存。", Control: "size", Editable: true},
	"cache.memory.ttl":                      {Group: "memory", Title: "内存缓存 TTL", Description: "内存缓存对象有效期。", Control: "duration", Editable: true},
	"cache.memory.max_items":                {Group: "memory", Title: "内存缓存对象数", Description: "进程内最多保留的对象数量。", Control: "number", Editable: true},
	"transport.timeout":                     {Group: "transport", Title: "出站请求超时", Description: "访问上游镜像站或代理目标的 HTTP client 超时。", Control: "duration", Editable: true},
	"transport.idle_conn_timeout":           {Group: "transport", Title: "空闲连接超时", Description: "出站 HTTP 连接池空闲连接保留时间。", Control: "duration", Editable: true},
	"transport.max_idle_conns":              {Group: "transport", Title: "最大空闲连接数", Description: "出站 HTTP client 连接池大小。", Control: "number", Editable: true},
	"apk.enabled":                           {Group: "apk", Title: "启用 APK 缓存", Description: "是否处理 Alpine APK 请求。", Control: "toggle", Editable: true},
	"apk.verify_hash":                       {Group: "apk", Title: "校验 APK Hash", Description: "使用 APKINDEX 中的 hash 校验包文件。", Control: "toggle", Editable: true},
	"apk.verify_signature":                  {Group: "apk", Title: "校验 APK 签名", Description: "使用 keys_dir 中的公钥校验 APK archive 签名。", Control: "toggle", Editable: true},
	"apk.keys_dir":                          {Group: "apk", Title: "APK 公钥目录", Description: "Alpine RSA 公钥目录，保存后会重载 verifier。", Control: "path", Editable: true},
	"apt.enabled":                           {Group: "apt", Title: "启用 APT 缓存", Description: "是否处理 APT 代理和镜像站请求。", Control: "toggle", Editable: true},
	"apt.verify_hash":                       {Group: "apt", Title: "校验 APT Hash", Description: "使用 Release/Packages/by-hash 校验 APT 文件。", Control: "toggle", Editable: true},
	"apt.load_index_async":                  {Group: "apt", Title: "异步加载 APT 索引", Description: "索引下载后在后台解析 expected hash。", Control: "toggle", Editable: true},
	"proxy.enabled":                         {Group: "proxy", Title: "启用通用代理", Description: "是否允许非包请求走通用 HTTP 代理。", Control: "toggle", Editable: true},
	"proxy.allow_connect":                   {Group: "proxy", Title: "允许 CONNECT", Description: "是否允许 HTTPS CONNECT 隧道透传。", Control: "toggle", Editable: true},
	"proxy.cache_non_package_requests":      {Group: "proxy", Title: "缓存非包请求", Description: "是否缓存普通 GET/HEAD 代理响应。", Control: "toggle", Editable: true},
	"proxy.upstream_proxy":                  {Group: "proxy", Title: "出站代理", Description: "访问上游时使用的 socks5/http/https 代理。", Control: "url", Editable: true, Sensitive: true},
	"proxy.allowed_hosts":                   {Group: "proxy", Title: "旧版允许 Host", Description: "兼容字段，主入口请使用代理页面的白名单表格。", Control: "host_list", Editable: false},
	"hash_store.path":                       {Group: "hash_store", Title: "Hash Store 路径", Description: "Pebble hash 缓存路径，修改后需重启。", Control: "path", Editable: true},
	"hash_store.rebuild_on_corruption":      {Group: "hash_store", Title: "损坏后重建", Description: "Hash Store 打开失败时是否删除并重建，修改后需重启。", Control: "toggle", Editable: true},
	"hash_store.trust_file_stat":            {Group: "hash_store", Title: "信任文件 stat", Description: "实际 hash 缓存命中时是否信任 size/mtime。", Control: "toggle", Editable: true},
	"hash_store.actual_revalidate_interval": {Group: "hash_store", Title: "实际 Hash 复算间隔", Description: "缓存文件实际 hash 的重新计算间隔。", Control: "duration", Editable: true},
}

func findSettingDef(key string) *settingDef {
	for idx := range settingDefs {
		if settingDefs[idx].key == key {
			return &settingDefs[idx]
		}
	}
	return nil
}

func ListSettingSchema() []SettingSchema {
	out := make([]SettingSchema, 0, len(settingDefs))
	for _, def := range settingDefs {
		meta := metaForSetting(def)
		out = append(out, SettingSchema{
			Key:             def.key,
			Group:           meta.Group,
			Title:           meta.Title,
			Description:     meta.Description,
			ValueType:       def.valueType,
			Control:         meta.Control,
			Editable:        meta.Editable,
			HotReload:       !def.restartRequired,
			RestartRequired: def.restartRequired,
			Sensitive:       meta.Sensitive,
		})
	}
	return out
}

func metaForSetting(def settingDef) settingMeta {
	if meta, ok := settingMetas[def.key]; ok {
		if meta.Group == "" {
			meta.Group = defaultSettingGroup(def.key)
		}
		if meta.Title == "" {
			meta.Title = def.key
		}
		if meta.Control == "" {
			meta.Control = defaultSettingControl(def.valueType)
		}
		return meta
	}
	return settingMeta{
		Group:    defaultSettingGroup(def.key),
		Title:    def.key,
		Control:  defaultSettingControl(def.valueType),
		Editable: true,
	}
}

func settingEditable(def *settingDef) bool {
	if def == nil {
		return false
	}
	return metaForSetting(*def).Editable
}

func defaultSettingGroup(key string) string {
	group := key
	for idx, char := range key {
		if char == '.' {
			group = key[:idx]
			break
		}
	}
	return group
}

func defaultSettingControl(valueType string) string {
	switch valueType {
	case "bool":
		return "toggle"
	case "int":
		return "number"
	case "string[]":
		return "list"
	default:
		return "text"
	}
}

func stringSetting(key string, restart bool, field func(*config.Config) *string) settingDef {
	return settingDef{
		key:             key,
		valueType:       "string",
		restartRequired: restart,
		marshal: func(c *config.Config) (json.RawMessage, error) {
			return json.Marshal(*field(c))
		},
		apply: func(c *config.Config, raw json.RawMessage) error {
			var value string
			if err := json.Unmarshal(raw, &value); err != nil {
				return err
			}
			*field(c) = value
			return nil
		},
	}
}

func boolSetting(key string, restart bool, field func(*config.Config) *bool) settingDef {
	return settingDef{
		key:             key,
		valueType:       "bool",
		restartRequired: restart,
		marshal: func(c *config.Config) (json.RawMessage, error) {
			return json.Marshal(*field(c))
		},
		apply: func(c *config.Config, raw json.RawMessage) error {
			var value bool
			if err := json.Unmarshal(raw, &value); err != nil {
				return err
			}
			*field(c) = value
			return nil
		},
	}
}

func intSetting(key string, restart bool, field func(*config.Config) *int) settingDef {
	return settingDef{
		key:             key,
		valueType:       "int",
		restartRequired: restart,
		marshal: func(c *config.Config) (json.RawMessage, error) {
			return json.Marshal(*field(c))
		},
		apply: func(c *config.Config, raw json.RawMessage) error {
			var value int
			if err := json.Unmarshal(raw, &value); err != nil {
				return err
			}
			if value < 0 {
				return fmt.Errorf("must be >= 0")
			}
			*field(c) = value
			return nil
		},
	}
}

func stringSliceSetting(key string, restart bool, field func(*config.Config) *[]string) settingDef {
	return settingDef{
		key:             key,
		valueType:       "string[]",
		restartRequired: restart,
		marshal: func(c *config.Config) (json.RawMessage, error) {
			return json.Marshal(*field(c))
		},
		apply: func(c *config.Config, raw json.RawMessage) error {
			var value []string
			if err := json.Unmarshal(raw, &value); err != nil {
				return err
			}
			*field(c) = value
			return nil
		},
	}
}
