package policy

import (
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Policy 缓存策略类型
type Policy string

const (
	PolicyDefault   Policy = "default"
	PolicySizeBased Policy = "size"
	PolicyTypeBased Policy = "type"
	PolicyFrequency Policy = "frequency"
	PolicyAdaptive  Policy = "adaptive"
)

// Priority 缓存优先级
type Priority int

const (
	PriorityLow Priority = iota
	PriorityNormal
	PriorityHigh
)

// FileCategory 文件类型分类
type FileCategory int

const (
	CategoryUnknown FileCategory = iota
	CategoryIndex
	CategoryPackage
	CategorySmall
	CategoryMedium
	CategoryLarge
)

// Config 缓存策略配置
type Config struct {
	Policy             Policy
	SizeThresholds     SizeThresholdConfig
	TypeRules          []TypeRuleConfig
	FrequencyThresholds FrequencyThresholdConfig
	AdaptiveConfig     AdaptiveConfig
}

type SizeThresholdConfig struct {
	Small  string
	Medium string
	Large  string
}

type TypeRuleConfig struct {
	Pattern     string
	Priority    string
	TTL         string
	MemoryCache bool
	Preload     bool
}

type FrequencyThresholdConfig struct {
	Hot  int
	Cold int
}

type AdaptiveConfig struct {
	Enabled        bool
	AdjustInterval string
	SampleSize    int
	PriorityBoost int
}

// FilePolicy 文件缓存策略
type FilePolicy struct {
	Category    FileCategory
	Priority    Priority
	TTL         time.Duration
	MemoryCache bool
	Preload     bool
}

// Manager 缓存策略管理器
type Manager struct {
	mu            sync.RWMutex
	config        Config
	sizeThresholds SizeThresholds
	typeRules     []CompiledTypeRule
	accessStats   map[string]*AccessStats
	lastAdjust    time.Time
}

// CompiledTypeRule 编译后的类型规则
type CompiledTypeRule struct {
	Pattern    *regexp.Regexp
	Priority   Priority
	TTL        time.Duration
	MemoryCache bool
	Preload    bool
}

// AccessStats 访问统计
type AccessStats struct {
	AccessCount   int64
	LastAccess    time.Time
	DailyAccess   int
	DailyCountMap map[time.Time]int
}

// SizeThresholds 大小阈值（字节）
type SizeThresholds struct {
	Small  int64
	Medium int64
	Large  int64
}

// New 创建缓存策略管理器
func New(config Config) *Manager {
	m := &Manager{
		config:      config,
		accessStats: make(map[string]*AccessStats),
	}

	m.sizeThresholds = SizeThresholds{
		Small:  parseSize(config.SizeThresholds.Small, 1<<20),
		Medium: parseSize(config.SizeThresholds.Medium, 10<<20),
		Large:  parseSize(config.SizeThresholds.Large, 100<<20),
	}

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
		m.typeRules = append(m.typeRules, compiled)
	}

	if config.AdaptiveConfig.Enabled {
		go m.startAdaptiveAdjustment()
	}

	return m
}

// GetFilePolicy 获取文件的缓存策略
func (m *Manager) GetFilePolicy(filePath string, fileSize int64) FilePolicy {
	m.mu.RLock()
	defer m.mu.RUnlock()

	policy := FilePolicy{
		Category:    m.getFileCategory(filePath, fileSize),
		Priority:    PriorityNormal,
		TTL:         0,
		MemoryCache: true,
	}

	switch m.config.Policy {
	case PolicySizeBased:
		policy = m.applySizeBasedPolicy(policy, fileSize)
	case PolicyTypeBased:
		policy = m.applyTypeBasedPolicy(policy, filePath)
	case PolicyFrequency:
		policy = m.applyFrequencyBasedPolicy(policy, filePath)
	case PolicyAdaptive:
		policy = m.applyAdaptivePolicy(policy, filePath, fileSize)
	}

	return policy
}

// getFileCategory 获取文件分类
func (m *Manager) getFileCategory(filePath string, fileSize int64) FileCategory {
	baseName := filepath.Base(filePath)

	// 检查是否是索引文件
	if baseName == "APKINDEX.tar.gz" || baseName == "APKINDEX.tar.gz_old" {
		return CategoryIndex
	}

	// 基于大小分类
	if fileSize < m.sizeThresholds.Small {
		return CategorySmall
	} else if fileSize < m.sizeThresholds.Medium {
		return CategoryMedium
	}
	return CategoryLarge
}

// applySizeBasedPolicy 应用基于大小的策略
func (m *Manager) applySizeBasedPolicy(policy FilePolicy, fileSize int64) FilePolicy {
	switch policy.Category {
	case CategorySmall:
		policy.Priority = PriorityHigh
		policy.MemoryCache = true
		if policy.TTL == 0 {
			policy.TTL = 24 * time.Hour
		}
	case CategoryMedium:
		policy.Priority = PriorityNormal
		if policy.TTL == 0 {
			policy.TTL = 7 * 24 * time.Hour
		}
	case CategoryLarge:
		policy.Priority = PriorityLow
		policy.MemoryCache = false
		if policy.TTL == 0 {
			policy.TTL = 30 * 24 * time.Hour
		}
	case CategoryIndex:
		policy.Priority = PriorityHigh
	}
	return policy
}

// applyTypeBasedPolicy 应用基于类型的策略
func (m *Manager) applyTypeBasedPolicy(policy FilePolicy, filePath string) FilePolicy {
	baseName := filepath.Base(filePath)

	for _, rule := range m.typeRules {
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
func (m *Manager) applyFrequencyBasedPolicy(policy FilePolicy, filePath string) FilePolicy {
	stats := m.getAccessStats(filePath)
	dailyAccess := stats.GetDailyAccess()

	hotThreshold := m.config.FrequencyThresholds.Hot
	if hotThreshold == 0 {
		hotThreshold = 100
	}

	coldThreshold := m.config.FrequencyThresholds.Cold
	if coldThreshold == 0 {
		coldThreshold = 1
	}

	if dailyAccess >= hotThreshold {
		policy.Priority = PriorityHigh
		policy.MemoryCache = true
	} else if dailyAccess <= coldThreshold {
		policy.Priority = PriorityLow
		policy.MemoryCache = false
	}

	return policy
}

// applyAdaptivePolicy 应用自适应策略
func (m *Manager) applyAdaptivePolicy(policy FilePolicy, filePath string, fileSize int64) FilePolicy {
	policy = m.applySizeBasedPolicy(policy, fileSize)
	policy = m.applyFrequencyBasedPolicy(policy, filePath)
	policy = m.applyTypeBasedPolicy(policy, filePath)

	return policy
}

// RecordAccess 记录文件访问
func (m *Manager) RecordAccess(filePath string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	stats := m.getAccessStatsLocked(filePath)
	stats.AccessCount++
	stats.LastAccess = time.Now()

	// 记录每日访问
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	if stats.DailyCountMap == nil {
		stats.DailyCountMap = make(map[time.Time]int)
	}
	stats.DailyCountMap[today]++

	// 清理旧的统计数据
	m.cleanupOldStats()
}

// getAccessStats 获取访问统计
func (m *Manager) getAccessStats(filePath string) *AccessStats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.getAccessStatsLocked(filePath)
}

// getAccessStatsLocked 获取访问统计（Caller must hold lock）
func (m *Manager) getAccessStatsLocked(filePath string) *AccessStats {
	stats, exists := m.accessStats[filePath]
	if !exists {
		stats = &AccessStats{
			DailyCountMap: make(map[time.Time]int),
		}
		m.accessStats[filePath] = stats
	}
	return stats
}

// cleanupOldStats 清理旧的统计数据
func (m *Manager) cleanupOldStats() {
	cutoff := time.Now().AddDate(0, 0, -7)
	for _, stats := range m.accessStats {
		for date := range stats.DailyCountMap {
			if date.Before(cutoff) {
				delete(stats.DailyCountMap, date)
			}
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
func (m *Manager) startAdaptiveAdjustment() {
	interval := m.config.AdaptiveConfig.AdjustInterval
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
		m.adjustPolicies()
	}
}

// adjustPolicies 调整策略
func (m *Manager) adjustPolicies() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.lastAdjust = time.Now()

	// 收集所有文件的访问统计
	type fileStat struct {
		path  string
		stats *AccessStats
	}

	var allStats []fileStat
	for path, stats := range m.accessStats {
		allStats = append(allStats, fileStat{path, stats})
	}

	// 按每日访问次数排序
	sort.Slice(allStats, func(i, j int) bool {
		return allStats[i].stats.GetDailyAccess() > allStats[j].stats.GetDailyAccess()
	})

	// 获取阈值
	sampleSize := m.config.AdaptiveConfig.SampleSize
	if sampleSize <= 0 {
		sampleSize = 100
	}

	hotCount := sampleSize / 10
	if hotCount < 1 {
		hotCount = 1
	}

	// TODO: 根据统计数据调整策略
}

// GetPolicyInfo 获取策略信息
func (m *Manager) GetPolicyInfo() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	info := make(map[string]interface{})
	info["policy"] = m.config.Policy
	info["size_thresholds"] = map[string]int64{
		"small":  m.sizeThresholds.Small,
		"medium": m.sizeThresholds.Medium,
		"large":  m.sizeThresholds.Large,
	}
	info["tracked_files"] = len(m.accessStats)
	info["last_adjust"] = m.lastAdjust

	return info
}

// ShouldPreload 判断是否应该预加载
func (m *Manager) ShouldPreload(filePath string) bool {
	policy := m.GetFilePolicy(filePath, 0)
	return policy.Preload
}

// ShouldCacheInMemory 判断是否应该缓存到内存
func (m *Manager) ShouldCacheInMemory(filePath string, fileSize int64) bool {
	policy := m.GetFilePolicy(filePath, fileSize)
	return policy.MemoryCache
}

// GetPriority 获取文件优先级
func (m *Manager) GetPriority(filePath string, fileSize int64) Priority {
	policy := m.GetFilePolicy(filePath, fileSize)
	return policy.Priority
}

// GetTTL 获取文件TTL
func (m *Manager) GetTTL(filePath string, fileSize int64) time.Duration {
	policy := m.GetFilePolicy(filePath, fileSize)
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
func parsePriority(priorityStr string) Priority {
	switch strings.ToLower(priorityStr) {
	case "low":
		return PriorityLow
	case "high":
		return PriorityHigh
	default:
		return PriorityNormal
	}
}
