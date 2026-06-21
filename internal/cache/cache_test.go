package cache

import (
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseSize(t *testing.T) {
	got, err := ParseSize("256MB")
	if err != nil {
		t.Fatal(err)
	}
	if got != 256<<20 {
		t.Fatalf("got %d", got)
	}
}

func TestMemoryCacheLRUAndTTL(t *testing.T) {
	c := NewMemory(8, 2, 20*time.Millisecond, nil)
	defer c.Stop()

	if !c.Set("a", []byte("1234"), http.Header{"A": []string{"1"}}, http.StatusOK, time.Now()) {
		t.Fatal("set a")
	}
	if !c.Set("b", []byte("12"), nil, http.StatusOK, time.Now()) {
		t.Fatal("set b")
	}
	if _, ok := c.Get("a"); !ok {
		t.Fatal("a should exist")
	}
	if !c.Set("c", []byte("12"), nil, http.StatusOK, time.Now()) {
		t.Fatal("set c")
	}
	if _, ok := c.Get("b"); ok {
		t.Fatal("b should be evicted as least recently used")
	}
	time.Sleep(30 * time.Millisecond)
	if _, ok := c.Get("a"); ok {
		t.Fatal("a should expire")
	}
}

func TestMemoryCacheDeleteStatsAndOversize(t *testing.T) {
	c := NewMemory(4, 10, 0, nil)
	defer c.Stop()
	if c.Set("large", []byte("12345"), nil, http.StatusOK, time.Now()) {
		t.Fatal("oversized item should be rejected")
	}
	if !c.Set("a", []byte("12"), nil, http.StatusAccepted, time.Now()) {
		t.Fatal("set a")
	}
	current, max, items := c.Stats()
	if current != 2 || max != 4 || items != 1 {
		t.Fatalf("stats=%d,%d,%d", current, max, items)
	}
	c.Delete("a")
	if _, ok := c.Get("a"); ok {
		t.Fatal("deleted item found")
	}
}

func TestKeyLocksSerializeSameKeyOnly(t *testing.T) {
	locks := NewKeyLocks()
	firstUnlock := locks.Lock("same")
	var acquired atomic.Bool
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		unlock := locks.Lock("same")
		acquired.Store(true)
		unlock()
	}()
	time.Sleep(20 * time.Millisecond)
	if acquired.Load() {
		t.Fatal("same key lock acquired before release")
	}
	firstUnlock()
	wg.Wait()
	if !acquired.Load() {
		t.Fatal("same key lock did not acquire after release")
	}

	unlockA := locks.Lock("a")
	unlockB := locks.Lock("b")
	unlockB()
	unlockA()
}

func TestParseSizeRejectsInvalidAndNegative(t *testing.T) {
	if _, err := ParseSize("nope"); err == nil {
		t.Fatal("expected invalid size error")
	}
	if _, err := ParseSize("-1"); err == nil {
		t.Fatal("expected negative size error")
	}
}

func TestDeleteExpired(t *testing.T) {
	c := NewMemory(0, 0, time.Millisecond, nil)
	defer c.Stop()
	if !c.Set("a", []byte("x"), nil, http.StatusOK, time.Now()) {
		t.Fatal("set a")
	}
	time.Sleep(2 * time.Millisecond)
	c.deleteExpired()
	current, _, items := c.Stats()
	if current != 0 || items != 0 {
		t.Fatalf("expired item remained: current=%d items=%d", current, items)
	}
}
