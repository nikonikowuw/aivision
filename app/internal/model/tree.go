package model

import "sort"

type TreeNode[T any] struct {
	Item     T
	Children []*TreeNode[T]
}

// BuildTree 将一组元素组装为树（按 sort,id 升序）。
// 脏数据环：断环——父链中出现环时停止遍历，当前元素自身作为根，避免产出互为父子的环。
func BuildTree[T any](items []T, id func(T) uint64, parentID func(T) uint64, sortVal func(T) int) []*TreeNode[T] {
	n := len(items)
	indexMap := make(map[uint64]int, n)
	for i, item := range items {
		indexMap[id(item)] = i
	}

	parentOf := func(i int) int {
		pID := parentID(items[i])
		if pID == 0 {
			return -1
		}
		if pIndex, ok := indexMap[pID]; ok {
			return pIndex
		}
		return -1
	}

	keyOf := func(i int) (int, uint64) {
		return sortVal(items[i]), id(items[i])
	}

	children := make([][]int, n)
	pathStamp := make([]int, n)
	pathID := 0
	var roots []int

	for i := 0; i < n; i++ {
		if pathStamp[i] != 0 {
			continue
		}
		pathID++
		current := i
		for {
			pathStamp[current] = pathID
			p := parentOf(current)
			if p < 0 {
				roots = append(roots, current)
				break
			}
			if pathStamp[p] == pathID {
				roots = append(roots, current)
				break
			}
			if pathStamp[p] != 0 {
				children[p] = append(children[p], current)
				break
			}
			children[p] = append(children[p], current)
			current = p
		}
	}

	for i := 0; i < n; i++ {
		sortTreeByKey(children[i], keyOf)
	}
	sortTreeByKey(roots, keyOf)

	nodes := make([]*TreeNode[T], n)
	for i, item := range items {
		nodes[i] = &TreeNode[T]{Item: item}
	}
	for i := 0; i < n; i++ {
		for _, childIdx := range children[i] {
			nodes[i].Children = append(nodes[i].Children, nodes[childIdx])
		}
	}

	var result []*TreeNode[T]
	for _, rootIdx := range roots {
		result = append(result, nodes[rootIdx])
	}
	return result
}

// sortTreeByKey 按 keyOf 排序键升序排序（sort 优先，同值按 id 稳定）。
func sortTreeByKey(items []int, keyOf func(i int) (int, uint64)) {
	sort.Slice(items, func(a, b int) bool {
		sa, ia := keyOf(items[a])
		sb, ib := keyOf(items[b])
		if sa != sb {
			return sa < sb
		}
		return ia < ib
	})
}

// IsDescendant 判断 candidateID 是否位于 ancestorID 的子树中。
// parentOf 返回指定 id 的父 id；不存在时返回 false。
// 父链中出现脏数据环时停止遍历，避免校验本身陷入死循环。
func IsDescendant(parentOf func(id uint64) (uint64, bool), ancestorID, candidateID uint64) bool {
	visited := make(map[uint64]struct{})
	for current := candidateID; current != 0; {
		if current == ancestorID {
			return true
		}
		if _, ok := visited[current]; ok {
			return false
		}
		visited[current] = struct{}{}
		parent, ok := parentOf(current)
		if !ok {
			return false
		}
		current = parent
	}
	return false
}
