package utils

// Set 泛型集合类型，基于 map[T]struct{} 实现
type Set[T comparable] struct {
	data map[T]struct{}
}

// NewSet 创建一个新的空集合
func NewSet[T comparable]() *Set[T] {
	return &Set[T]{
		data: make(map[T]struct{}),
	}
}

// NewSetFromSlice 从切片创建集合
func NewSetFromSlice[T comparable](items []T) *Set[T] {
	set := NewSet[T]()
	for _, item := range items {
		set.Add(item)
	}
	return set
}

// Add 向集合中添加元素
func (s *Set[T]) Add(item T) {
	s.data[item] = struct{}{}
}

// Remove 从集合中移除元素
func (s *Set[T]) Remove(item T) {
	delete(s.data, item)
}

// Contains 检查集合是否包含指定元素
func (s *Set[T]) Contains(item T) bool {
	_, exists := s.data[item]
	return exists
}

// Size 返回集合中元素的数量
func (s *Set[T]) Size() int {
	return len(s.data)
}

// IsEmpty 检查集合是否为空
func (s *Set[T]) IsEmpty() bool {
	return len(s.data) == 0
}

// Clear 清空集合
func (s *Set[T]) Clear() {
	s.data = make(map[T]struct{})
}

// ToSlice 将集合转换为切片
func (s *Set[T]) ToSlice() []T {
	result := make([]T, 0, len(s.data))
	for item := range s.data {
		result = append(result, item)
	}
	return result
}

// Union 返回两个集合的并集
func (s *Set[T]) Union(other *Set[T]) *Set[T] {
	result := NewSet[T]()
	for item := range s.data {
		result.Add(item)
	}
	for item := range other.data {
		result.Add(item)
	}
	return result
}

// Intersection 返回两个集合的交集
func (s *Set[T]) Intersection(other *Set[T]) *Set[T] {
	result := NewSet[T]()
	// 遍历较小的集合以提高性能
	if s.Size() <= other.Size() {
		for item := range s.data {
			if other.Contains(item) {
				result.Add(item)
			}
		}
	} else {
		for item := range other.data {
			if s.Contains(item) {
				result.Add(item)
			}
		}
	}
	return result
}

// Difference 返回两个集合的差集 (s - other)
func (s *Set[T]) Difference(other *Set[T]) *Set[T] {
	result := NewSet[T]()
	for item := range s.data {
		if !other.Contains(item) {
			result.Add(item)
		}
	}
	return result
}

// IsSubset 检查当前集合是否是另一个集合的子集
func (s *Set[T]) IsSubset(other *Set[T]) bool {
	if s.Size() > other.Size() {
		return false
	}
	for item := range s.data {
		if !other.Contains(item) {
			return false
		}
	}
	return true
}

// IsSuperset 检查当前集合是否是另一个集合的超集
func (s *Set[T]) IsSuperset(other *Set[T]) bool {
	return other.IsSubset(s)
}

// Equal 检查两个集合是否相等
func (s *Set[T]) Equal(other *Set[T]) bool {
	if s.Size() != other.Size() {
		return false
	}
	for item := range s.data {
		if !other.Contains(item) {
			return false
		}
	}
	return true
}

// ForEach 对集合中的每个元素执行函数
func (s *Set[T]) ForEach(fn func(item T)) {
	for item := range s.data {
		fn(item)
	}
}

// Filter 根据条件过滤集合元素
func (s *Set[T]) Filter(fn func(item T) bool) *Set[T] {
	result := NewSet[T]()
	for item := range s.data {
		if fn(item) {
			result.Add(item)
		}
	}
	return result
}

// Clone 创建集合的副本
func (s *Set[T]) Clone() *Set[T] {
	result := NewSet[T]()
	for item := range s.data {
		result.Add(item)
	}
	return result
}

// String 返回集合的字符串表示（用于调试）
func (s *Set[T]) String() string {
	// 这里返回一个简单的描述，实际使用时可以根据需要格式化
	return "Set{size: " + string(rune(s.Size())) + "}"
}
