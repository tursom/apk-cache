package i18n

import (
	_ "embed"
	"encoding/json"
	"log"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

//go:embed locales/en.toml
var enToml []byte

//go:embed locales/zh.toml
var zhToml []byte

var localizer *i18n.Localizer

func init() {
	// 在未手动加载 i18n 时保证不会 panic
	initLocale("")
}

// detectLocale 自动检测系统语言
func detectLocale(locale string) string {
	// 支持的语言列表
	supported := map[string]bool{
		"zh": true,
		"en": true,
	}

	// 如果命令行参数已指定，检查是否支持
	if locale != "" {
		if supported[locale] {
			return locale
		}
		// 如果不支持，继续检测环境变量
	}

	// 按优先级检查环境变量
	envVars := []string{"LC_ALL", "LC_MESSAGES", "LANG"}
	for _, env := range envVars {
		if val := os.Getenv(env); val != "" {
			// 解析语言代码，如 "zh_CN.UTF-8" -> "zh"
			lang := strings.Split(val, ".")[0] // 去除编码部分
			lang = strings.Split(lang, "_")[0] // 去除地区部分
			lang = strings.ToLower(lang)

			if supported[lang] {
				return lang
			}
		}
	}

	// 默认使用英语
	return "en"
}

func Init(locale string) {
	detectedLocale := initLocale(locale)

	log.Println(T("UsingLanguage", map[string]any{"Lang": detectedLocale}))
}

func initLocale(locale string) string {
	bundle := i18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("toml", toml.Unmarshal)

	// 自动检测语言
	detectedLocale := detectLocale(locale)

	// 只加载必要的语言文件
	switch detectedLocale {
	case "zh":
		bundle.MustParseMessageFileBytes(zhToml, "zh.toml")
	default:
		// 默认加载英语，包括检测到英语或未知语言的情况
		bundle.MustParseMessageFileBytes(enToml, "en.toml")
	}

	localizer = i18n.NewLocalizer(bundle, detectedLocale)

	return detectedLocale
}

func T(messageID string, templateData map[string]any) string {
	msg, err := localizer.Localize(&i18n.LocalizeConfig{
		MessageID:    messageID,
		TemplateData: templateData,
	})
	if err != nil {
		// 在未命中时将 templateData 以 JSON 格式添加到返回值中以便调试
		if len(templateData) > 0 {
			jsonData, err := json.Marshal(templateData)
			if err == nil {
				return messageID + " " + string(jsonData)
			}
		}
		return messageID // 回退到 ID
	}
	return msg
}
