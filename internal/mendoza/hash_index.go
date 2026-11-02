package mendoza

// HashIndex looks up entries of a document by hash. Every lookup returns entries in ascending order.
//
// Each list is stored as a linked chain in a shared node slice instead of as a
// []int per hash. Build reuses the nodes and map storage when the index is
// rebuilt for another document, though older Go map implementations may still allocate.
type HashIndex struct {
	children map[childKey]chain
	xorData  map[Hash]chain
	nodes    []node
}

// childKey identifies the children of parent whose hash is hash. Keying by parent keeps lookups proportional to
// the matches in one container, even when a value (such as a repeated "_type") occurs throughout the document.
type childKey struct {
	parent int
	hash   Hash
}

type chain struct{ head, tail int }
type node struct{ idx, next int }

// Build (re)builds the index for hashList, reusing the memory of any previous build.
func (hashIndex *HashIndex) Build(hashList *HashList) {
	if hashIndex.children == nil {
		hashIndex.children = map[childKey]chain{}
		hashIndex.xorData = map[Hash]chain{}
	}
	// Maps never shrink, but clearing a map that once held a large document stays cheap: with Go 1.24+ maps,
	// clearing 100k of retained capacity measured ~0.35µs. Re-making the maps when they are oversized would
	// allocate on every large/small alternation, so they are kept.
	clear(hashIndex.children)
	clear(hashIndex.xorData)
	hashIndex.nodes = hashIndex.nodes[:0]

	for idx := range hashList.Entries {
		entry := &hashList.Entries[idx]
		pushChain(hashIndex, hashIndex.children, childKey{parent: entry.Parent, hash: entry.Hash}, idx)

		if !entry.XorHash.IsNull() {
			for it := hashList.Iter(idx); !it.IsDone(); it.Next() {
				xorHash := entry.XorHash
				xorHash.Xor(it.GetEntry().Hash)

				if current, ok := hashIndex.xorData[xorHash]; ok && hashIndex.nodes[current.tail].idx == idx {
					// Already present.
					continue
				}

				pushChain(hashIndex, hashIndex.xorData, xorHash, idx)
			}
		}
	}
}

func pushChain[K comparable](hashIndex *HashIndex, m map[K]chain, key K, idx int) {
	n := len(hashIndex.nodes)
	hashIndex.nodes = append(hashIndex.nodes, node{idx: idx, next: -1})
	if current, ok := m[key]; ok {
		hashIndex.nodes[current.tail].next = n
		current.tail = n
		m[key] = current
	} else {
		m[key] = chain{head: n, tail: n}
	}
}

// LookupChild returns the children of the map or slice entry parent whose hash is key.
func (hashIndex *HashIndex) LookupChild(parent int, key Hash) ChainIter {
	return iterChain(hashIndex, hashIndex.children, childKey{parent: parent, hash: key})
}

// LookupXor returns the entries which have a field that, when removed, makes their XorHash equal to key.
func (hashIndex *HashIndex) LookupXor(key Hash) ChainIter {
	return iterChain(hashIndex, hashIndex.xorData, key)
}

func iterChain[K comparable](hashIndex *HashIndex, m map[K]chain, key K) ChainIter {
	if current, ok := m[key]; ok {
		return ChainIter{nodes: hashIndex.nodes, cur: current.head}
	}
	return ChainIter{cur: -1}
}

// ChainIter iterates over the entry indices returned by a lookup.
type ChainIter struct {
	nodes []node
	cur   int
}

// Next returns the next entry index, or false when there are no more.
func (it *ChainIter) Next() (int, bool) {
	if it.cur < 0 {
		return 0, false
	}
	n := it.nodes[it.cur]
	it.cur = n.next
	return n.idx, true
}
