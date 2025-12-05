package apt

import (
	"testing"
)

// 测试用例数据
var testCases = []struct {
	name     string
	input    string
	expected string
}{
	{"Empty", "", "default"},
	{"SimpleHost", "example.com", "example.com"},
	{"WithColon", "example.com:8080", "example.com_8080"},
	{"WithSlash", "example.com/path", "example.com_path"},
	{"WithBackslash", "example.com\\path", "example.com_path"},
	{"DoubleDot", "example..com", "example_com"},
	{"WithAsterisk", "example*.com", "example_.com"},
	{"WithQuestion", "example?.com", "example_.com"},
	{"WithQuotes", "example\".com", "example_.com"},
	{"WithAngleBrackets", "example<.com", "example_.com"},
	{"WithPipe", "example|.com", "example_.com"},
	// {"MultipleSpecialChars", "http://example.com:8080/path/to/file", "http___example.com_8080_path_to_file"},
	// {"ComplexCase", "https://user:pass@example.com:443/path/../file?query=value", "https___user_pass@example.com_443_path___file_query=value"},
}

// BenchmarkSanitizeHostForPathOriginal 基准测试原始版本
func BenchmarkSanitizeHostForPathOriginal(b *testing.B) {
	for i := 0; i < b.N; i++ {
		for _, tc := range testCases {
			sanitizeHostForPathOriginal(tc.input)
		}
	}
}

// BenchmarkSanitizeHostForPathOptimized 基准测试优化版本
func BenchmarkSanitizeHostForPathOptimized(b *testing.B) {
	for i := 0; i < b.N; i++ {
		for _, tc := range testCases {
			sanitizeHostForPathOptimized(tc.input)
		}
	}
}

// TestBothImplementations 测试两个实现是否产生相同的结果
func TestBothImplementations(t *testing.T) {
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			original := sanitizeHostForPathOriginal(tc.input)
			optimized := sanitizeHostForPathOptimized(tc.input)

			if original != optimized {
				t.Errorf("实现不一致: 输入=%q, 原始=%q, 优化=%q",
					tc.input, original, optimized)
			}

			if optimized != tc.expected {
				t.Errorf("优化版本结果不正确: 输入=%q, 期望=%q, 实际=%q",
					tc.input, tc.expected, optimized)
			}
		})
	}
}

// BenchmarkSingleCaseOriginal 单个测试用例的原始版本基准测试
func BenchmarkSingleCaseOriginal(b *testing.B) {
	for i := 0; i < b.N; i++ {
		for _, tc := range testCases {
			sanitizeHostForPathOriginal(tc.input)
		}
	}
}

// BenchmarkSingleCaseOptimized 单个测试用例的优化版本基准测试
func BenchmarkSingleCaseOptimized(b *testing.B) {
	for i := 0; i < b.N; i++ {
		for _, tc := range testCases {
			sanitizeHostForPathOptimized(tc.input)
		}
	}
}
