package i18n

import (
	"os"
	"testing"
)

func TestDetectLocale(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		param    string
		expected string
	}{
		{
			name:     "Command line parameter takes precedence",
			param:    "zh",
			expected: "zh",
		},
		{
			name:     "LC_ALL environment variable",
			envVars:  map[string]string{"LC_ALL": "zh_CN.UTF-8"},
			expected: "zh",
		},
		{
			name:     "LC_MESSAGES environment variable",
			envVars:  map[string]string{"LC_MESSAGES": "zh_CN.UTF-8"},
			expected: "zh",
		},
		{
			name:     "LANG environment variable",
			envVars:  map[string]string{"LANG": "zh_CN.UTF-8"},
			expected: "zh",
		},
		{
			name:     "English environment variable",
			envVars:  map[string]string{"LANG": "en_US.UTF-8"},
			expected: "en",
		},
		{
			name:     "Unknown language defaults to English",
			envVars:  map[string]string{"LANG": "fr_FR.UTF-8"},
			expected: "en",
		},
		{
			name:     "No environment variables defaults to English",
			expected: "en",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 设置环境变量
			for key, value := range tt.envVars {
				os.Setenv(key, value)
				defer os.Unsetenv(key)
			}

			result := detectLocale(tt.param)
			if result != tt.expected {
				t.Errorf("detectLocale(%q) = %q, want %q", tt.param, result, tt.expected)
			}
		})
	}
}

func TestInitLocaleLoadsOnlyNecessaryLanguages(t *testing.T) {
	// 测试命令行参数优先
	t.Run("Command line parameter loads Chinese", func(t *testing.T) {
		locale := initLocale("zh")
		if locale != "zh" {
			t.Errorf("Expected locale 'zh', got '%s'", locale)
		}

		// 验证翻译功能正常工作
		msg := T("ServerStarted", map[string]any{"Addr": ":8080"})
		expected := "APK 缓存服务器启动在 :8080"
		if msg != expected {
			t.Errorf("Expected '%s', got '%s'", expected, msg)
		}
	})

	// 测试英语环境
	t.Run("Command line parameter loads English", func(t *testing.T) {
		locale := initLocale("en")
		if locale != "en" {
			t.Errorf("Expected locale 'en', got '%s'", locale)
		}

		// 验证翻译功能正常工作
		msg := T("ServerStarted", map[string]any{"Addr": ":8080"})
		expected := "APK cache server started on :8080"
		if msg != expected {
			t.Errorf("Expected '%s', got '%s'", expected, msg)
		}
	})

	// 测试未知语言环境（默认英语）
	t.Run("Unknown locale defaults to English", func(t *testing.T) {
		locale := initLocale("fr")
		if locale != "en" {
			t.Errorf("Expected locale 'en', got '%s'", locale)
		}

		// 验证翻译功能正常工作
		msg := T("ServerStarted", map[string]any{"Addr": ":8080"})
		expected := "APK cache server started on :8080"
		if msg != expected {
			t.Errorf("Expected '%s', got '%s'", expected, msg)
		}
	})
}
