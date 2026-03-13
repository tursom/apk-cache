package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type TranslationFile struct {
	Messages map[string]map[string]string
}

type AnalysisResult struct {
	usedInCode    map[string]bool
	definedInEn   map[string]bool
	definedInZh   map[string]bool
	missingInEn   []string
	missingInZh   []string
	unusedInEn    []string
	unusedInZh    []string
}

func main() {
	dir := flag.String("dir", ".", "Directory to analyze")
	flag.Parse()

	if err := analyzeI18n(*dir); err != nil {
		log.Fatal(err)
	}
}

func analyzeI18n(rootDir string) error {
	// Find all Go source files
	goFiles, err := findGoFiles(rootDir)
	if err != nil {
		return fmt.Errorf("failed to find Go files: %w", err)
	}

	// Find i18n locale files
	localeDir := filepath.Join(rootDir, "utils", "i18n", "locales")
	enFile := filepath.Join(localeDir, "en.toml")
	zhFile := filepath.Join(localeDir, "zh.toml")

	// Scan code for i18n.T() calls
	usedKeys, err := scanCodeForI18n(goFiles)
	if err != nil {
		return fmt.Errorf("failed to scan code: %w", err)
	}

	// Parse translation files
	enKeys, err := parseTomlFile(enFile)
	if err != nil {
		return fmt.Errorf("failed to parse %s: %w", enFile, err)
	}

	zhKeys, err := parseTomlFile(zhFile)
	if err != nil {
		return fmt.Errorf("failed to parse %s: %w", zhFile, err)
	}

	// Analyze differences
	result := analyze(usedKeys, enKeys, zhKeys)

	// Print results
	printResults(result)

	return nil
}

func findGoFiles(rootDir string) ([]string, error) {
	var files []string
	absDir, _ := filepath.Abs(rootDir)

	// Find all .go files recursively
	err := filepath.Walk(absDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip vendor, i18n-analyzer and hidden directories
		if info.IsDir() {
			name := info.Name()
			if name == "vendor" || name == ".git" || name == "i18n-analyzer" || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
		}

		// Only process .go files
		if !info.IsDir() && strings.HasSuffix(path, ".go") {
			files = append(files, path)
		}

		return nil
	})

	return files, err
}

func scanCodeForI18n(files []string) (map[string]bool, error) {
	// Match i18n.T("key", ...)
	pattern := regexp.MustCompile(`i18n\.T\("([^"]+)"`)

	usedKeys := make(map[string]bool)

	for _, file := range files {
		// Skip i18n-analyzer directory only
		if strings.Contains(file, "i18n-analyzer") {
			continue
		}

		content, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", file, err)
		}

		var matches [][]string

		// For i18n.go itself, match both i18n.T() and T()
		if strings.HasSuffix(file, "i18n.go") {
			patternLocal := regexp.MustCompile(`\bT\("([^"]+)"`)
			matches = patternLocal.FindAllStringSubmatch(string(content), -1)
		} else {
			matches = pattern.FindAllStringSubmatch(string(content), -1)
		}

		for _, match := range matches {
			if len(match) > 1 {
				usedKeys[match[1]] = true
			}
		}
	}

	return usedKeys, nil
}

func parseTomlFile(path string) (map[string]bool, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	keys := make(map[string]bool)

	// Simple TOML parsing - look for [section] patterns
	sectionPattern := regexp.MustCompile(`^\[([^\]]+)\]`)
	lines := strings.Split(string(content), "\n")

	var currentSection string
	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Skip comments and empty lines
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}

		// Check for section headers
		matches := sectionPattern.FindStringSubmatch(line)
		if len(matches) > 1 {
			currentSection = matches[1]
			keys[currentSection] = true
		}
	}

	return keys, nil
}

func analyze(used, en, zh map[string]bool) *AnalysisResult {
	result := &AnalysisResult{
		usedInCode:    used,
		definedInEn:   en,
		definedInZh:   zh,
		missingInEn:   []string{},
		missingInZh:   []string{},
		unusedInEn:    []string{},
		unusedInZh:    []string{},
	}

	// Find missing translations (used in code but not in toml)
	for key := range used {
		if !en[key] {
			result.missingInEn = append(result.missingInEn, key)
		}
		if !zh[key] {
			result.missingInZh = append(result.missingInZh, key)
		}
	}

	// Find unused translations (in toml but not used in code)
	for key := range en {
		if !used[key] {
			result.unusedInEn = append(result.unusedInEn, key)
		}
	}
	for key := range zh {
		if !used[key] {
			result.unusedInZh = append(result.unusedInZh, key)
		}
	}

	// Sort for consistent output
	sort.Strings(result.missingInEn)
	sort.Strings(result.missingInZh)
	sort.Strings(result.unusedInEn)
	sort.Strings(result.unusedInZh)

	return result
}

func printResults(r *AnalysisResult) {
	fmt.Println("╔════════════════════════════════════════════════════════════╗")
	fmt.Println("║                  i18n Analysis Report                      ║")
	fmt.Println("╚════════════════════════════════════════════════════════════╝")
	fmt.Println()

	// Summary
	fmt.Println("📊 Summary:")
	fmt.Printf("   Keys used in code:     %d\n", len(r.usedInCode))
	fmt.Printf("   Keys in en.toml:       %d\n", len(r.definedInEn))
	fmt.Printf("   Keys in zh.toml:       %d\n", len(r.definedInZh))
	fmt.Println()

	// Missing translations
	fmt.Println("🔴 MISSING TRANSLATIONS (used in code but not in file):")
	fmt.Println()

	if len(r.missingInEn) > 0 {
		fmt.Printf("   📝 en.toml: %d missing\n", len(r.missingInEn))
		for _, key := range r.missingInEn {
			fmt.Printf("      - %s\n", key)
		}
		fmt.Println()
	} else {
		fmt.Println("   ✅ en.toml: All translations complete!")
		fmt.Println()
	}

	if len(r.missingInZh) > 0 {
		fmt.Printf("   📝 zh.toml: %d missing\n", len(r.missingInZh))
		for _, key := range r.missingInZh {
			fmt.Printf("      - %s\n", key)
		}
		fmt.Println()
	} else {
		fmt.Println("   ✅ zh.toml: All translations complete!")
		fmt.Println()
	}

	// Unused translations
	fmt.Println("🟡 UNUSED TRANSLATIONS (in file but not used in code):")
	fmt.Println()

	if len(r.unusedInEn) > 0 {
		fmt.Printf("   📝 en.toml: %d unused\n", len(r.unusedInEn))
		for _, key := range r.unusedInEn {
			fmt.Printf("      - %s\n", key)
		}
		fmt.Println()
	} else {
		fmt.Println("   ✅ en.toml: No unused translations!")
		fmt.Println()
	}

	if len(r.unusedInZh) > 0 {
		fmt.Printf("   📝 zh.toml: %d unused\n", len(r.unusedInZh))
		for _, key := range r.unusedInZh {
			fmt.Printf("      - %s\n", key)
		}
		fmt.Println()
	} else {
		fmt.Println("   ✅ zh.toml: No unused translations!")
		fmt.Println()
	}

	// Exit code based on missing translations
	if len(r.missingInEn) > 0 || len(r.missingInZh) > 0 {
		fmt.Println("⚠️  WARNING: Missing translations detected!")
		os.Exit(1)
	}
}
