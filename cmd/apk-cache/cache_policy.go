package main

import (
	"log"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tursom/apk-cache/utils"
	"github.com/tursom/apk-cache/utils/i18n"
)

// CachePolicy 缓存策略类型
type CachePolicy string

const (
	// PolicyDefault 默认策略
	PolicyDefault CachePolicy = "default"
	// PolicySizeBased 基于大小的策略
	PolicySizeBased CachePolicy = "size"
	// PolicyTypeBased 基于类型的策略
	PolicyTypeBased CachePolicy = "type"
	// PolicyFrequencyBased 基于访问频率的策略
	PolicyFrequencyBased CachePolicy = "frequency"
	// PolicyAdaptive 自适应策略
	PolicyAdaptive CachePolicy = "adaptive"
)

// CachePriority 缓存优先级
type CachePriority int

const (
	// PriorityLow 低优先级
	PriorityLow CachePriority = iota
	// PriorityNormal 正常优先级
	PriorityNormal
	// PriorityHigh 高优先级
	PriorityHigh
)

// FileCategory 文件类型分类
type FileCategory int

const (
	// CategoryUnknown 未知类型
	CategoryUnknown FileCategory = iota
	// CategoryIndex 索引文件
	CategoryIndex
	// CategoryPackage 包文件
	CategoryPackage
	// CategorySmall 小文件 (< 1MB)
	CategorySmall
	// CategoryMedium 中等文件 (1-10MB)
	CategoryMedium
	// CategoryLarge 大文件 (> 10MB)
	CategoryLarge
)

// CachePolicyConfig 缓存策略配置
type CachePolicyConfig struct {
	// Policy 策略类型
	Policy CachePolicy `toml:"policy"`
	// SizeThresholds 文件大小阈值配置
	SizeThresholds SizeThresholdConfig `toml:"size_thresholds"`
	// TypeRules 类型规则配置
	TypeRules []TypeRuleConfig `toml:"type_rules"`
	// FrequencyThresholds 访问频率阈值
	FrequencyThresholds FrequencyThresholdConfig `toml:"frequency_thresholds"`
	// AdaptiveConfig 自适应配置
	AdaptiveConfig AdaptivePolicyConfig `toml:"adaptive"`
}

// SizeThresholdConfig 大小阈值配置
type SizeThresholdConfig struct {
	Small  string `toml:"small"`  // 小文件阈值，默认 "1MB"
	Medium string `toml:"medium"` // 中等文件阈值，默认 "10MB"
	Large  string `toml:"large"`  // 大文件阈值，默认 "100MB"
}

// TypeRuleConfig 类型规则配置
type TypeRuleConfig struct {
	Pattern    string `toml:"pattern"`    // 文件名匹配模式（正则）
	Priority   string `toml:"priority"`   // 优先级 (low, normal, high)
	TTL        string `toml:"ttl"`        // 缓存时间
	MemoryCache bool  `toml:"memory_cache"` // 是否启用内存缓存
	Preload    bool   `toml:"preload"`    // 是否预加载
}

// FrequencyThresholdConfig 访问频率阈值配置
type FrequencyThresholdConfig struct {
	Hot    int `toml:"hot"`    // 热门文件阈值（每天访问次数）
	Cold   int `toml:"cold"`    // 冷门文件阈值（每天访问次数）
}

// AdaptivePolicyConfig 自适应策略配置
type AdaptivePolicyConfig struct {
	Enabled         bool   `toml:"enabled"`
	AdjustInterval  string `toml:"adjust_interval"` // 调整间隔
	SampleSize      int    `toml:"sample_size"`    // 采样大小
	PriorityBoost   int    `toml:"priority_boost"`  // 优先级提升增量
}

// FilePolicy 文件缓存策略
type FilePolicy struct {
	Category    FileCategory
	Priority    CachePriority
	TTL         time.Duration
	MemoryCache bool
	Preload     bool
}

// FineGrainedCachePolicy 细粒度缓存策略管理器
type FineGrainedCachePolicy struct {
	mu           sync.RWMutex
	config       CachePolicyConfig
	sizeThresholds SizeThresholds
	typeRules    []CompiledTypeRule
	accessStats  map[string]*AccessStats
	lastAdjust   time.Time
}

// CompiledTypeRule 编译后的类型规则
type CompiledTypeRule struct {
	Pattern   *regexp.Regexp
	Priority  CachePriority
	TTL       time.Duration
	MemoryCache bool
	Preload   bool
}

// AccessStats 访问统计
type AccessStats struct {
	AccessCount   int64
	LastAccess    time.Time
	DailyAccess   int
	DailyCountMap map[time.Time]int // 按天统计
}

// SizeThresholds 大小阈值（字节）
type SizeThresholds struct {
	Small  int64
	Medium int64
	Large  int64
}

// 全局细粒度缓存策略管理器
var fineGrainedPolicy *FineGrainedCachePolicy

// NewFineGrainedCachePolicy 创建细粒度缓存策略管理器
func NewFineGrainedCachePolicy(config CachePolicyConfig) *FineGrainedCachePolicy {
	policy := &FineGrainedCachePolicy{
		config:      config,
		accessStats: make(map[string]*AccessStats),
	}

	// 初始化大小阈值
	policy.sizeThresholds = SizeThresholds{
		Small:  parseSize(config.SizeThresholds.Small, 1<<20),   // 1MB
		Medium: parseSize(config.SizeThresholds.Medium, 10<<20), // 10MB
		Large:  parseSize(config.SizeThresholds.Large, 100<<20), // 100MB
	}

	// 编译类型规则
	for _, rule := range config.TypeRules {
		compiled := CompiledTypeRule{
			Priority:    parsePriority(rule.Priority),
			TTL:        parseTTL(rule.TTL),
			MemoryCache: rule.MemoryCache,
			Preload:    rule.Preload,
		}
		if rule.Pattern != "" {
			compiled.Pattern = regexp.MustCompile(rule.Pattern)
		}
		policy.typeRules = append(policy.typeRules, compiled)
	}

	// 启动自适应调整
	if config.AdaptiveConfig.Enabled {
		go policy.startAdaptiveAdjustment()
	}

	return policy
}

// GetFilePolicy 获取文件的缓存策略
func (f *FineGrainedCachePolicy) GetFilePolicy(filePath string, fileSize int64) FilePolicy {
	f.mu.RLock()
	defer f.mu.RUnlock()

	policy := FilePolicy{
		Category:    f.getFileCategory(filePath, fileSize),
		Priority:    PriorityNormal,
		TTL:         *pkgCacheDuration,
		MemoryCache: *memoryCacheEnabled,
	}

	switch f.config.Policy {
	case PolicySizeBased:
		policy = f.applySizeBasedPolicy(policy, fileSize)
	case PolicyTypeBased:
		policy = f.applyTypeBasedPolicy(policy, filePath)
	case PolicyFrequencyBased:
		policy = f.applyFrequencyBasedPolicy(policy, filePath)
	case PolicyAdaptive:
		policy = f.applyAdaptivePolicy(policy, filePath, fileSize)
	default:
		// 使用默认策略
	}

	return policy
}

// getFileCategory 获取文件分类
func (f *FineGrainedCachePolicy) getFileCategory(filePath string, fileSize int64) FileCategory {
	// 检查是否是索引文件
	if utils.IsIndexFile(filePath) {
		return CategoryIndex
	}

	// 基于大小分类
	if fileSize < f.sizeThresholds.Small {
		return CategorySmall
	} else if fileSize < f.sizeThresholds.Medium {
		return CategoryMedium
	}
	return CategoryLarge
}

// applySizeBasedPolicy 应用基于大小的策略
func (f *FineGrainedCachePolicy) applySizeBasedPolicy(policy FilePolicy, fileSize int64) FilePolicy {
	switch policy.Category {
	case CategorySmall:
		// 小文件：使用较短TTL，高优先级，确保在内存缓存中
		policy.Priority = PriorityHigh
		policy.MemoryCache = true
		// 小文件默认1天TTL
		if policy.TTL == 0 {
			policy.TTL = 24 * time.Hour
		}
	case CategoryMedium:
		// 中等文件：正常优先级，正常TTL
		policy.Priority = PriorityNormal
		if policy.TTL == 0 {
			policy.TTL = 7 * 24 * time.Hour
		}
	case CategoryLarge:
		// 大文件：低优先级，较长TTL，不使用内存缓存
		policy.Priority = PriorityLow
		policy.MemoryCache = false
		if policy.TTL == 0 {
			policy.TTL = 30 * 24 * time.Hour
		}
	case CategoryIndex:
		// 索引文件：使用配置的索引缓存时间
		policy.Priority = PriorityHigh
		policy.TTL = *indexCacheDuration
	}
	return policy
}

// applyTypeBasedPolicy 应用基于类型的策略
func (f *FineGrainedCachePolicy) applyTypeBasedPolicy(policy FilePolicy, filePath string) FilePolicy {
	baseName := filepath.Base(filePath)

	for _, rule := range f.typeRules {
		if rule.Pattern != nil && rule.Pattern.MatchString(baseName) {
			policy.Priority = rule.Priority
			if rule.TTL > 0 {
				policy.TTL = rule.TTL
			}
			policy.MemoryCache = rule.MemoryCache
			policy.Preload = rule.Preload
			break
		}
	}

	return policy
}

// applyFrequencyBasedPolicy 应用基于访问频率的策略
func (f *FineGrainedCachePolicy) applyFrequencyBasedPolicy(policy FilePolicy, filePath string) FilePolicy {
	stats := f.getAccessStats(filePath)
	dailyAccess := stats.GetDailyAccess()

	hotThreshold := f.config.FrequencyThresholds.Hot
	if hotThreshold == 0 {
		hotThreshold = 100 // 默认热门阈值
	}

	coldThreshold := f.config.FrequencyThresholds.Cold
	if coldThreshold == 0 {
		coldThreshold = 1 // 默认冷门阈值
	}

	if dailyAccess >= hotThreshold {
		// 热门文件：高优先级，确保在内存缓存中
		policy.Priority = PriorityHigh
		policy.MemoryCache = true
	} else if dailyAccess <= coldThreshold {
		// 冷门文件：低优先级
		policy.Priority = PriorityLow
		policy.MemoryCache = false
	}

	return policy
}

// applyAdaptivePolicy 应用自适应策略
func (f *FineGrainedCachePolicy) applyAdaptivePolicy(policy FilePolicy, filePath string, fileSize int64) FilePolicy {
	// 先应用大小策略
	policy = f.applySizeBasedPolicy(policy, fileSize)

	// 再根据访问频率调整
	policy = f.applyFrequencyBasedPolicy(policy, filePath)

	// 根据文件类型规则调整
	policy = f.applyTypeBasedPolicy(policy, filePath)

	return policy
}

// RecordAccess 记录文件访问
func (f *FineGrainedCachePolicy) RecordAccess(filePath string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	stats := f.getAccessStatsLocked(filePath)
	stats.AccessCount++
	stats.LastAccess = time.Now()

	// 记录每日访问
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	if stats.DailyCountMap == nil {
		stats.DailyCountMap = make(map[time.Time]int)
	}
	stats.DailyCountMap[today]++

	// 清理旧的统计数据（保留7天）
	f.cleanupOldStats()
}

// getAccessStats 获取访问统计（非线程安全）
func (f *FineGrainedCachePolicy) getAccessStats(filePath string) *AccessStats {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.getAccessStatsLocked(filePath)
}

// getAccessStatsLocked 获取访问统计（Caller must hold lock）
func (f *FineGrainedCachePolicy) getAccessStatsLocked(filePath string) *AccessStats {
	stats, exists := f.accessStats[filePath]
	if !exists {
		stats = &AccessStats{
			DailyCountMap: make(map[time.Time]int),
		}
		f.accessStats[filePath] = stats
	}
	return stats
}

// cleanupOldStats 清理旧的统计数据
func (f *FineGrainedCachePolicy) cleanupOldStats() {
	cutoff := time.Now().AddDate(0, 0, -7)
	for _, stats := range f.accessStats {
		for date := range stats.DailyCountMap {
			if date.Before(cutoff) {
				delete(stats.DailyCountMap, date)
			}
		}
		// 如果统计数据为空，删除整个记录
		if len(stats.DailyCountMap) == 0 && stats.AccessCount > 0 {
			// 保留 AccessCount，只清理每日统计
		}
	}
}

// GetDailyAccess 获取每日访问次数
func (s *AccessStats) GetDailyAccess() int {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return s.DailyCountMap[today]
}

// startAdaptiveAdjustment 启动自适应调整
func (f *FineGrainedCachePolicy) startAdaptiveAdjustment() {
	interval := f.config.AdaptiveConfig.AdjustInterval
	if interval == "" {
		interval = "1h"
	}

	duration, err := time.ParseDuration(interval)
	if err != nil {
		duration = time.Hour
	}

	ticker := time.NewTicker(duration)
	defer ticker.Stop()

	for range ticker.C {
		f.adjustPolicies()
	}
}

// adjustPolicies 调整策略
func (f *FineGrainedCachePolicy) adjustPolicies() {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.lastAdjust = time.Now()

	// 收集所有文件的访问统计
	type fileStat struct {
		path  string
		stats *AccessStats
	}

	var allStats []fileStat
	for path, stats := range f.accessStats {
		allStats = append(allStats, fileStat{path, stats})
	}

	// 按每日访问次数排序
	sort.Slice(allStats, func(i, j int) bool {
		return allStats[i].stats.GetDailyAccess() > allStats[j].stats.GetDailyAccess()
	})

	// 获取阈值
	sampleSize := f.config.AdaptiveConfig.SampleSize
	if sampleSize <= 0 {
		sampleSize = 100
	}

	hotCount := sampleSize / 10 // 前10%为热门
	if hotCount < 1 {
		hotCount = 1
	}

	log.Println(i18n.T("FineGrainedPolicyAdjusted", map[string]any{
		"TotalFiles": len(allStats),
		"HotFiles":   hotCount,
	}))

	// 记录调整日志
	utils.Monitoring.RecordPolicyAdjustment("adaptive", int64(len(allStats)))
}

// GetPolicyInfo 获取策略信息
func (f *FineGrainedCachePolicy) GetPolicyInfo() map[string]interface{} {
	f.mu.RLock()
	defer f.mu.RUnlock()

	info := make(map[string]interface{})
	info["policy"] = f.config.Policy
	info["size_thresholds"] = map[string]int64{
		"small":  f.sizeThresholds.Small,
		"medium": f.sizeThresholds.Medium,
		"large":  f.sizeThresholds.Large,
	}
	info["tracked_files"] = len(f.accessStats)
	info["last_adjust"] = f.lastAdjust

	return info
}

// ShouldPreload 判断是否应该预加载
func (f *FineGrainedCachePolicy) ShouldPreload(filePath string) bool {
	policy := f.GetFilePolicy(filePath, 0)
	return policy.Preload
}

// ShouldCacheInMemory 判断是否应该缓存到内存
func (f *FineGrainedCachePolicy) ShouldCacheInMemory(filePath string, fileSize int64) bool {
	policy := f.GetFilePolicy(filePath, fileSize)
	return policy.MemoryCache
}

// GetPriority 获取文件优先级
func (f *FineGrainedCachePolicy) GetPriority(filePath string, fileSize int64) CachePriority {
	policy := f.GetFilePolicy(filePath, fileSize)
	return policy.Priority
}

// GetTTL 获取文件TTL
func (f *FineGrainedCachePolicy) GetTTL(filePath string, fileSize int64) time.Duration {
	policy := f.GetFilePolicy(filePath, fileSize)
	return policy.TTL
}

// 辅助函数

// parseSize 解析大小字符串
func parseSize(sizeStr string, defaultSize int64) int64 {
	if sizeStr == "" {
		return defaultSize
	}

	sizeStr = strings.TrimSpace(sizeStr)
	re := regexp.MustCompile(`^(\d+(?:\.\d+)?)\s*(B|KB|MB|GB|TB)?$`)
	matches := re.FindStringSubmatch(sizeStr)
	if matches == nil {
		return defaultSize
	}

	value, _ := strconv.ParseFloat(matches[1], 64)
	unit := strings.ToUpper(matches[2])

	switch unit {
	case "B", "":
		return int64(value)
	case "KB":
		return int64(value * 1024)
	case "MB":
		return int64(value * 1024 * 1024)
	case "GB":
		return int64(value * 1024 * 1024 * 1024)
	case "TB":
		return int64(value * 1024 * 1024 * 1024 * 1024)
	default:
		return defaultSize
	}
}

// parseTTL 解析TTL字符串
func parseTTL(ttlStr string) time.Duration {
	if ttlStr == "" {
		return 0
	}

	duration, err := time.ParseDuration(ttlStr)
	if err != nil {
		return 0
	}
	return duration
}

// parsePriority 解析优先级字符串
func parsePriority(priorityStr string) CachePriority {
	switch strings.ToLower(priorityStr) {
	case "low":
		return PriorityLow
	case "high":
		return PriorityHigh
	default:
		return PriorityNormal
	}
}
