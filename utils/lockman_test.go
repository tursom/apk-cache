package utils

import (
	"sync"
	"testing"
	"time"
)

func TestFileLockManagerConcurrent(t *testing.T) {
	manager := NewFileLockManager()
	const goroutines = 1000
	const path = "test-file"

	var wg sync.WaitGroup
	counter := 0

	// 并发测试
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			unlock := manager.Acquire(path)
			defer unlock()

			// 临界区
			temp := counter
			time.Sleep(time.Microsecond)
			counter = temp + 1
		}()
	}

	wg.Wait()

	if counter != goroutines {
		t.Errorf("Race condition! Expected %d, got %d", goroutines, counter)
	}

	if size := manager.Size(); size != 0 {
		t.Errorf("Memory leak! Expected 0 locks, got %d", size)
	}
}

func TestFileLockManagerMultipleFiles(t *testing.T) {
	manager := NewFileLockManager()

	// 测试多个文件的并发访问
	var wg sync.WaitGroup
	files := []string{"file1", "file2", "file3"}

	for _, file := range files {
		wg.Add(1)
		go func(f string) {
			defer wg.Done()
			unlock := manager.Acquire(f)
			defer unlock()
			time.Sleep(10 * time.Millisecond)
		}(file)
	}

	wg.Wait()

	if size := manager.Size(); size != 0 {
		t.Errorf("Expected 0 locks, got %d", size)
	}
}

func TestFileLockManagerCleanup(t *testing.T) {
	manager := NewFileLockManager()

	// 快速获取和释放
	for i := 0; i < 100; i++ {
		unlock := manager.Acquire("test")
		unlock()
	}

	if size := manager.Size(); size != 0 {
		t.Errorf("Locks not cleaned up: %d remaining", size)
	}
}
