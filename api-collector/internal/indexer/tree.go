package indexer

import "cmp"

type Node[K cmp.Ordered, V any] struct {
	isLeaf   bool
	keys     []K
	children []*Node[K, V]
	values   []V
	parent   *Node[K, V]
	next     *Node[K, V]
}

type BPlusTree[K cmp.Ordered, V any] struct {
	root    *Node[K, V]
	maxKeys int
}

type Entry[K cmp.Ordered, V any] struct {
	Key   K
	Value V
}

func NewBPlusTree[K cmp.Ordered, V any](order int) *BPlusTree[K, V] {
	if order < 3 {
		order = 3
	}

	return &BPlusTree[K, V]{
		root: &Node[K, V]{
			isLeaf: true,
		},
		maxKeys: order - 1,
	}
}
func (t *BPlusTree[K, V]) Find(key K) (V, bool) {
	leaf := t.findLeaf(key)

	for i, k := range leaf.keys {
		if k == key {
			return leaf.values[i], true
		}
	}

	var zero V
	return zero, false
}

func (t *BPlusTree[K, V]) FindFloor(key K) (K, V, bool) {
	leaf := t.findLeaf(key)

	for i := len(leaf.keys) - 1; i >= 0; i-- {
		if leaf.keys[i] <= key {
			return leaf.keys[i], leaf.values[i], true
		}
	}

	prev := t.previousLeaf(leaf)

	if prev != nil && len(prev.keys) > 0 {
		i := len(prev.keys) - 1

		return prev.keys[i], prev.values[i], true
	}

	var zeroKey K
	var zeroValue V

	return zeroKey, zeroValue, false
}

func (t *BPlusTree[K, V]) Insert(key K, value V) {
	leaf := t.findLeaf(key)

	for i, existingKey := range leaf.keys {
		if existingKey == key {
			leaf.values[i] = value
			return
		}
	}

	insertIndex := 0

	for insertIndex < len(leaf.keys) &&
		leaf.keys[insertIndex] < key {
		insertIndex++
	}

	leaf.keys = insertAt(
		leaf.keys,
		insertIndex,
		key,
	)

	leaf.values = insertAt(
		leaf.values,
		insertIndex,
		value,
	)

	if len(leaf.keys) > t.maxKeys {
		t.splitLeaf(leaf)
	}
}

func (t *BPlusTree[K, V]) Range(
	start K,
	end K,
) []Entry[K, V] {
	if end < start {
		return nil
	}

	leaf := t.findLeaf(start)

	results := make(
		[]Entry[K, V],
		0,
	)

	for leaf != nil {
		for i, key := range leaf.keys {
			if key < start {
				continue
			}

			if key > end {
				return results
			}

			results = append(
				results,
				Entry[K, V]{
					Key:   key,
					Value: leaf.values[i],
				},
			)
		}

		leaf = leaf.next
	}

	return results
}

func (t *BPlusTree[K, V]) Last() (K, V, bool) {
	curr := t.root

	if curr == nil {
		var zeroKey K
		var zeroValue V
		return zeroKey, zeroValue, false
	}

	for !curr.isLeaf {
		curr = curr.children[len(curr.children)-1]
	}

	if len(curr.keys) == 0 {
		var zeroKey K
		var zeroValue V
		return zeroKey, zeroValue, false
	}

	i := len(curr.keys) - 1

	return curr.keys[i], curr.values[i], true
}

func (t *BPlusTree[K, V]) findLeaf(key K) *Node[K, V] {
	curr := t.root

	for !curr.isLeaf {
		i := 0

		for i < len(curr.keys) &&
			key >= curr.keys[i] {
			i++
		}

		curr = curr.children[i]
	}

	return curr
}

func (t *BPlusTree[K, V]) splitLeaf(
	leaf *Node[K, V],
) {
	mid := len(leaf.keys) / 2

	right := &Node[K, V]{
		isLeaf: true,

		keys: append(
			[]K(nil),
			leaf.keys[mid:]...,
		),

		values: append(
			[]V(nil),
			leaf.values[mid:]...,
		),

		parent: leaf.parent,
		next:   leaf.next,
	}

	leaf.keys = leaf.keys[:mid]
	leaf.values = leaf.values[:mid]

	leaf.next = right

	separator := right.keys[0]

	t.insertIntoParent(
		leaf,
		separator,
		right,
	)
}

func (t *BPlusTree[K, V]) insertIntoParent(
	left *Node[K, V],
	key K,
	right *Node[K, V],
) {
	if left.parent == nil {
		root := &Node[K, V]{
			isLeaf: false,

			keys: []K{
				key,
			},

			children: []*Node[K, V]{
				left,
				right,
			},
		}

		left.parent = root
		right.parent = root

		t.root = root
		return
	}

	parent := left.parent

	childIndex := 0

	for childIndex < len(parent.children) &&
		parent.children[childIndex] != left {
		childIndex++
	}

	parent.keys = insertAt(
		parent.keys,
		childIndex,
		key,
	)

	parent.children = insertAt(
		parent.children,
		childIndex+1,
		right,
	)

	right.parent = parent

	if len(parent.keys) > t.maxKeys {
		t.splitInternal(parent)
	}
}

func (t *BPlusTree[K, V]) splitInternal(
	node *Node[K, V],
) {
	mid := len(node.keys) / 2

	promotedKey := node.keys[mid]

	right := &Node[K, V]{
		isLeaf: false,

		keys: append(
			[]K(nil),
			node.keys[mid+1:]...,
		),

		children: append(
			[]*Node[K, V](nil),
			node.children[mid+1:]...,
		),

		parent: node.parent,
	}

	node.keys = node.keys[:mid]
	node.children = node.children[:mid+1]

	for _, child := range right.children {
		child.parent = right
	}

	t.insertIntoParent(
		node,
		promotedKey,
		right,
	)
}

func (t *BPlusTree[K, V]) previousLeaf(
	target *Node[K, V],
) *Node[K, V] {
	curr := t.leftMostLeaf()

	var previous *Node[K, V]

	for curr != nil {
		if curr == target {
			return previous
		}

		previous = curr
		curr = curr.next
	}

	return nil
}

func (t *BPlusTree[K, V]) leftMostLeaf() *Node[K, V] {
	curr := t.root

	for !curr.isLeaf {
		curr = curr.children[0]
	}

	return curr
}

func insertAt[T any](
	values []T,
	index int,
	value T,
) []T {
	values = append(
		values,
		value,
	)

	copy(
		values[index+1:],
		values[index:len(values)-1],
	)

	values[index] = value

	return values
}
