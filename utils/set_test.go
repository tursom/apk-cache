package utils

import (
	"testing"
)

func TestNewSet(t *testing.T) {
	set := NewSet[int]()
	if set.Size() != 0 {
		t.Errorf("Expected empty set, got size %d", set.Size())
	}
	if !set.IsEmpty() {
		t.Error("Expected set to be empty")
	}
}

func TestAddAndContains(t *testing.T) {
	set := NewSet[string]()
	
	// 测试添加元素
	set.Add("apple")
	set.Add("banana")
	
	if !set.Contains("apple") {
		t.Error("Expected set to contain 'apple'")
	}
	if !set.Contains("banana") {
		t.Error("Expected set to contain 'banana'")
	}
	if set.Contains("orange") {
		t.Error("Set should not contain 'orange'")
	}
	
	if set.Size() != 2 {
		t.Errorf("Expected set size 2, got %d", set.Size())
	}
}

func TestRemove(t *testing.T) {
	set := NewSet[int]()
	set.Add(1)
	set.Add(2)
	set.Add(3)
	
	set.Remove(2)
	
	if set.Contains(2) {
		t.Error("Set should not contain 2 after removal")
	}
	if !set.Contains(1) {
		t.Error("Set should still contain 1")
	}
	if !set.Contains(3) {
		t.Error("Set should still contain 3")
	}
	if set.Size() != 2 {
		t.Errorf("Expected set size 2 after removal, got %d", set.Size())
	}
}

func TestNewSetFromSlice(t *testing.T) {
	items := []string{"a", "b", "c", "a"} // 包含重复元素
	set := NewSetFromSlice(items)
	
	if set.Size() != 3 {
		t.Errorf("Expected set size 3 (duplicates removed), got %d", set.Size())
	}
	if !set.Contains("a") || !set.Contains("b") || !set.Contains("c") {
		t.Error("Set should contain all unique elements from slice")
	}
}

func TestToSlice(t *testing.T) {
	set := NewSet[int]()
	set.Add(1)
	set.Add(2)
	set.Add(3)
	
	slice := set.ToSlice()
	
	if len(slice) != 3 {
		t.Errorf("Expected slice length 3, got %d", len(slice))
	}
	
	// 检查切片是否包含所有元素
	elementCount := make(map[int]int)
	for _, item := range slice {
		elementCount[item]++
	}
	
	for i := 1; i <= 3; i++ {
		if elementCount[i] != 1 {
			t.Errorf("Expected element %d to appear exactly once in slice", i)
		}
	}
}

func TestUnion(t *testing.T) {
	set1 := NewSetFromSlice([]int{1, 2, 3})
	set2 := NewSetFromSlice([]int{3, 4, 5})
	
	union := set1.Union(set2)
	
	expected := []int{1, 2, 3, 4, 5}
	if union.Size() != len(expected) {
		t.Errorf("Expected union size %d, got %d", len(expected), union.Size())
	}
	
	for _, item := range expected {
		if !union.Contains(item) {
			t.Errorf("Union should contain %d", item)
		}
	}
}

func TestIntersection(t *testing.T) {
	set1 := NewSetFromSlice([]string{"a", "b", "c"})
	set2 := NewSetFromSlice([]string{"b", "c", "d"})
	
	intersection := set1.Intersection(set2)
	
	if intersection.Size() != 2 {
		t.Errorf("Expected intersection size 2, got %d", intersection.Size())
	}
	if !intersection.Contains("b") {
		t.Error("Intersection should contain 'b'")
	}
	if !intersection.Contains("c") {
		t.Error("Intersection should contain 'c'")
	}
	if intersection.Contains("a") {
		t.Error("Intersection should not contain 'a'")
	}
	if intersection.Contains("d") {
		t.Error("Intersection should not contain 'd'")
	}
}

func TestDifference(t *testing.T) {
	set1 := NewSetFromSlice([]int{1, 2, 3, 4})
	set2 := NewSetFromSlice([]int{3, 4, 5, 6})
	
	diff := set1.Difference(set2)
	
	if diff.Size() != 2 {
		t.Errorf("Expected difference size 2, got %d", diff.Size())
	}
	if !diff.Contains(1) {
		t.Error("Difference should contain 1")
	}
	if !diff.Contains(2) {
		t.Error("Difference should contain 2")
	}
	if diff.Contains(3) {
		t.Error("Difference should not contain 3")
	}
}

func TestSubsetAndSuperset(t *testing.T) {
	set1 := NewSetFromSlice([]int{1, 2, 3})
	set2 := NewSetFromSlice([]int{1, 2, 3, 4, 5})
	
	if !set1.IsSubset(set2) {
		t.Error("set1 should be subset of set2")
	}
	if !set2.IsSuperset(set1) {
		t.Error("set2 should be superset of set1")
	}
	if set2.IsSubset(set1) {
		t.Error("set2 should not be subset of set1")
	}
}

func TestEqual(t *testing.T) {
	set1 := NewSetFromSlice([]string{"a", "b", "c"})
	set2 := NewSetFromSlice([]string{"c", "b", "a"})
	set3 := NewSetFromSlice([]string{"a", "b"})
	
	if !set1.Equal(set2) {
		t.Error("set1 and set2 should be equal (order doesn't matter)")
	}
	if set1.Equal(set3) {
		t.Error("set1 and set3 should not be equal")
	}
}

func TestClear(t *testing.T) {
	set := NewSetFromSlice([]int{1, 2, 3})
	set.Clear()
	
	if !set.IsEmpty() {
		t.Error("Set should be empty after clear")
	}
	if set.Size() != 0 {
		t.Errorf("Expected set size 0 after clear, got %d", set.Size())
	}
}

func TestForEach(t *testing.T) {
	set := NewSetFromSlice([]int{1, 2, 3})
	sum := 0
	
	set.ForEach(func(item int) {
		sum += item
	})
	
	if sum != 6 {
		t.Errorf("Expected sum 6, got %d", sum)
	}
}

func TestFilter(t *testing.T) {
	set := NewSetFromSlice([]int{1, 2, 3, 4, 5, 6})
	
	filtered := set.Filter(func(item int) bool {
		return item%2 == 0 // 只保留偶数
	})
	
	if filtered.Size() != 3 {
		t.Errorf("Expected filtered set size 3, got %d", filtered.Size())
	}
	if !filtered.Contains(2) || !filtered.Contains(4) || !filtered.Contains(6) {
		t.Error("Filtered set should contain even numbers only")
	}
	if filtered.Contains(1) || filtered.Contains(3) || filtered.Contains(5) {
		t.Error("Filtered set should not contain odd numbers")
	}
}

func TestClone(t *testing.T) {
	original := NewSetFromSlice([]string{"a", "b", "c"})
	clone := original.Clone()
	
	if !original.Equal(clone) {
		t.Error("Clone should be equal to original")
	}
	
	// 修改克隆不应该影响原始集合
	clone.Add("d")
	if original.Contains("d") {
		t.Error("Original set should not be affected by clone modification")
	}
}