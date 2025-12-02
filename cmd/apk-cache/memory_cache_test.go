package main

import (
	"bytes"
	"net/http"
	"testing"
	"time"

	"github.com/tursom/apk-cache/utils/i18n"
)

func init() {
	i18n.Init("zh")
}

func TestMemoryCacheBasicOperations(t *testing.T) {
	// 创建内存缓存
	cache := NewMemoryCache(10*1024*1024, 100, 30*time.Minute)

	// 测试数据
	testKey := "/test/package.apk"
	testData := []byte("test package data")
	testHeaders := map[string][]string{
		"Content-Type": {"application/octet-stream"},
		"ETag":         {"test-etag"},
	}

	// 测试 Set 和 Get
	if !cache.Set(testKey, testData, testHeaders, http.StatusOK, time.Now()) {
		t.Error("Failed to set cache item")
	}

	item, found := cache.Get(testKey)
	if !found {
		t.Error("Failed to get cache item")
	}

	if !bytes.Equal(item.Data, testData) {
		t.Error("Cached data doesn't match original data")
	}

	if item.StatusCode != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, item.StatusCode)
	}

	if item.Headers["Content-Type"][0] != "application/octet-stream" {
		t.Error("Headers not preserved correctly")
	}
}

func TestMemoryCacheExpiration(t *testing.T) {
	// 创建带短TTL的缓存
	cache := NewMemoryCache(10*1024*1024, 100, 100*time.Millisecond)

	testKey := "/test/expiring.apk"
	testData := []byte("expiring data")

	if !cache.Set(testKey, testData, nil, http.StatusOK, time.Now()) {
		t.Error("Failed to set cache item")
	}

	// 立即获取应该能找到
	_, found := cache.Get(testKey)
	if !found {
		t.Error("Cache item should be found immediately after setting")
	}

	// 等待过期
	time.Sleep(150 * time.Millisecond)

	// 再次获取应该找不到
	_, found = cache.Get(testKey)
	if found {
		t.Error("Cache item should be expired")
	}
}

func TestMemoryCacheEviction(t *testing.T) {
	// 创建小容量缓存以测试驱逐
	cache := NewMemoryCache(100, 2, 30*time.Minute)

	// 添加第一个项目
	if !cache.Set("/test/1.apk", []byte("data1"), nil, http.StatusOK, time.Now()) {
		t.Error("Failed to set first cache item")
	}

	// 添加第二个项目
	if !cache.Set("/test/2.apk", []byte("data2"), nil, http.StatusOK, time.Now()) {
		t.Error("Failed to set second cache item")
	}

	// 添加第三个项目，应该触发驱逐
	if !cache.Set("/test/3.apk", []byte("data3"), nil, http.StatusOK, time.Now()) {
		t.Error("Failed to set third cache item")
	}

	// 检查缓存大小
	currentSize, maxSize, itemCount, _ := cache.GetStats()
	if itemCount > 2 {
		t.Errorf("Expected max 2 items, got %d", itemCount)
	}

	if currentSize > maxSize {
		t.Errorf("Current size %d exceeds max size %d", currentSize, maxSize)
	}
}

func TestMemoryCacheTooLargeItem(t *testing.T) {
	// 创建小容量缓存
	cache := NewMemoryCache(10, 100, 30*time.Minute)

	// 尝试添加过大的项目
	largeData := []byte("this data is too large for the cache")
	result := cache.Set("/test/large.apk", largeData, nil, http.StatusOK, time.Now())

	if result {
		t.Error("Should not be able to set item larger than cache capacity")
	}
}

func TestMemoryCacheDelete(t *testing.T) {
	cache := NewMemoryCache(10*1024*1024, 100, 30*time.Minute)

	testKey := "/test/delete.apk"
	testData := []byte("data to delete")

	if !cache.Set(testKey, testData, nil, http.StatusOK, time.Now()) {
		t.Error("Failed to set cache item")
	}

	// 删除项目
	cache.Delete(testKey)

	// 检查是否已删除
	_, found := cache.Get(testKey)
	if found {
		t.Error("Cache item should be deleted")
	}
}

func TestMemoryCacheClear(t *testing.T) {
	cache := NewMemoryCache(10*1024*1024, 100, 30*time.Minute)

	// 添加多个项目
	for i := 0; i < 5; i++ {
		key := "/test/" + string(rune('a'+i)) + ".apk"
		cache.Set(key, []byte("data"), nil, http.StatusOK, time.Now())
	}

	// 清空缓存
	cache.Clear()

	// 检查缓存是否为空
	currentSize, _, itemCount, _ := cache.GetStats()
	if itemCount != 0 {
		t.Errorf("Expected 0 items after clear, got %d", itemCount)
	}
	if currentSize != 0 {
		t.Errorf("Expected 0 size after clear, got %d", currentSize)
	}
}

func TestMemoryCacheStats(t *testing.T) {
	cache := NewMemoryCache(10*1024*1024, 100, 30*time.Minute)

	// 添加一些数据
	for i := 0; i < 3; i++ {
		key := "/test/" + string(rune('a'+i)) + ".apk"
		data := []byte("test data " + string(rune('a'+i)))
		cache.Set(key, data, nil, http.StatusOK, time.Now())
	}

	// 获取统计信息
	currentSize, maxSize, itemCount, hitRate := cache.GetStats()

	if itemCount != 3 {
		t.Errorf("Expected 3 items, got %d", itemCount)
	}

	if maxSize != 10*1024*1024 {
		t.Errorf("Expected max size %d, got %d", 10*1024*1024, maxSize)
	}

	if currentSize <= 0 {
		t.Error("Current size should be positive")
	}

	// 命中率应该为0，因为我们还没有进行任何Get操作
	if hitRate != 0.0 {
		t.Errorf("Expected hit rate 0.0, got %f", hitRate)
	}
}
