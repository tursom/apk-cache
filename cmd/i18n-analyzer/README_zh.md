# i18n Analyzer

国际化翻译分析工具，用于检测代码中使用的 i18n 键与翻译文件中定义的键之间的差异。

## 功能

- 扫描 Go 代码中 `i18n.T("key")` 和 `T("key")` (i18n.go 内部) 的调用
- 解析 TOML 格式的翻译文件
- 检测缺失的翻译（代码中使用但翻译文件中没有）
- 检测未使用的翻译（翻译文件中存在但代码中未使用）

## 使用方法

```bash
# 构建
go build -o i18n-analyzer ./cmd/i18n-analyzer

# 运行
./i18n-analyzer -dir .
```

## 输出示例

```
╔════════════════════════════════════════════════════════════╗
║                  i18n Analysis Report                      ║
╚════════════════════════════════════════════════════════════╝

📊 Summary:
   Keys used in code:     174
   Keys in en.toml:       174
   Keys in zh.toml:       174

🔴 MISSING TRANSLATIONS (used in code but not in file):
   ✅ en.toml: All translations complete!
   ✅ zh.toml: All translations complete!

🟡 UNUSED TRANSLATIONS (in file but not used in code):
   ✅ en.toml: No unused translations!
   ✅ zh.toml: No unused translations!
```

## 退出码

- `0`: 所有翻译完整
- `1`: 存在缺失的翻译
