# i18n Analyzer

Internationalization translation analysis tool for detecting discrepancies between i18n keys used in code and those defined in translation files.

## Features

- Scans Go code for `i18n.T("key")` and `T("key")` (for i18n.go internal usage)
- Parses TOML format translation files
- Detects missing translations (used in code but not in translation files)
- Detects unused translations (in translation files but not used in code)

## Usage

```bash
# Build
go build -o i18n-analyzer ./cmd/i18n-analyzer

# Run
./i18n-analyzer -dir .
```

## Example Output

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

## Exit Codes

- `0`: All translations complete
- `1`: Missing translations detected
