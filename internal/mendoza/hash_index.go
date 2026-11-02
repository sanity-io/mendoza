package mendoza

// HashIndex looks up entries of a document by hash. Every lookup returns entries in ascending order.
//
// Each list is stored as a linked chain in a shared node slice instead of as a
// []int per hash. Build reuses the nodes and map storage when the index is
// rebuilt for another document, though older Go map implementations may still allocate.
type HashIndex struct {
	children map[childKey]chain
	xorData  map[childKey]chain
	nodes    []node
}

// childKey identifies the children of parent whose hash (or xor hash) is hash. Keying by parent keeps lookups
// proportional to the matches in one container, even when a value (such as a repeated "_type") occurs throughout
// the document.
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
		hashIndex.xorData = map[childKey]chain{}
	}
	// Maps never shrink, but clearing a map that once held a large document stays cheap: with Go 1.24+ maps,
	// clearing 100k of retained capacity measured ~0.35µs. Re-making the maps when they are oversized would
	// allocate on every large/small alternation, so they are kept.
	for key := range hashIndex.children {
		delete(hashIndex.children, key)
	}
	for key := range hashIndex.xorData {
		delete(hashIndex.xorData, key)
	}
	hashIndex.nodes = hashIndex.nodes[:0]

	for idx := range hashList.Entries {
		entry := &hashList.Entries[idx]
		key := childKey{parent: entry.Parent, hash: entry.Hash}
		current, ok := hashIndex.children[key]
		hashIndex.children[key] = hashIndex.pushChain(current, ok, idx)

		if !entry.XorHash.IsNull() {
			for it := hashList.Iter(idx); !it.IsDone(); it.Next() {
				xorKey := childKey{parent: entry.Parent, hash: entry.XorHash}
				xorKey.hash.Xor(it.GetEntry().Hash)

				current, ok := hashIndex.xorData[xorKey]
				if ok && hashIndex.nodes[current.tail].idx == idx {
					// Already present.
					continue
				}

				hashIndex.xorData[xorKey] = hashIndex.pushChain(current, ok, idx)
			}
		}
	}
}

// pushChain appends idx to a chain, or starts a new one when the map lookup found no entry.
func (hashIndex *HashIndex) pushChain(current chain, ok bool, idx int) chain {
	n := len(hashIndex.nodes)
	hashIndex.nodes = append(hashIndex.nodes, node{idx: idx, next: -1})
	if ok {
		hashIndex.nodes[current.tail].next = n
		current.tail = n
		return current
	}
	return chain{head: n, tail: n}
}

// LookupChild returns the children of the map or slice entry parent whose hash is key.
func (hashIndex *HashIndex) LookupChild(parent int, key Hash) ChainIter {
	current, ok := hashIndex.children[childKey{parent: parent, hash: key}]
	return hashIndex.iterChain(current, ok)
}

// LookupXor returns the children of parent which have a field that, when removed, makes their XorHash equal to key.
func (hashIndex *HashIndex) LookupXor(parent int, key Hash) ChainIter {
	current, ok := hashIndex.xorData[childKey{parent: parent, hash: key}]
	return hashIndex.iterChain(current, ok)
}

func (hashIndex *HashIndex) iterChain(current chain, ok bool) ChainIter {
	if ok {
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
