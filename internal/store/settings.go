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

type SettingSchema struct {
	Key             string `json:"key"`
	Group           string `json:"group"`
	ValueType       string `json:"value_type"`
	Editable        bool   `json:"editable"`
	HotReload       bool   `json:"hot_reload"`
	RestartRequired bool   `json:"restart_required"`
}

var settingDefs = []settingDef{
	stringSetting("server.listen", true, func(c *config.Config) *string { return &c.Server.Listen }),
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
		group := def.key
		for idx, char := range def.key {
			if char == '.' {
				group = def.key[:idx]
				break
			}
		}
		out = append(out, SettingSchema{
			Key:             def.key,
			Group:           group,
			ValueType:       def.valueType,
			Editable:        true,
			HotReload:       !def.restartRequired,
			RestartRequired: def.restartRequired,
		})
	}
	return out
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
