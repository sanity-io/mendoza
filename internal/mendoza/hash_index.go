package mendoza

// HashIndex maps a hash to the entries with that hash, in ascending order.
//
// Each list is stored as a linked chain in a shared node slice instead of as a
// []int per hash. This lets Build reuse all of its memory when the index is
// rebuilt for another document.
type HashIndex struct {
	data    map[Hash]chain
	xorData map[Hash]chain
	nodes   []node
}

type chain struct{ head, tail int }
type node struct{ idx, next int }

func NewHashIndex(hashList *HashList) *HashIndex {
	hashIndex := &HashIndex{}
	hashIndex.Build(hashList)
	return hashIndex
}

// Build (re)builds the index for hashList, reusing the memory of any previous build.
func (hashIndex *HashIndex) Build(hashList *HashList) {
	if hashIndex.data == nil {
		hashIndex.data = map[Hash]chain{}
		hashIndex.xorData = map[Hash]chain{}
	}
	for key := range hashIndex.data {
		delete(hashIndex.data, key)
	}
	for key := range hashIndex.xorData {
		delete(hashIndex.xorData, key)
	}
	hashIndex.nodes = hashIndex.nodes[:0]

	for idx := range hashList.Entries {
		entry := &hashList.Entries[idx]
		hashIndex.push(hashIndex.data, entry.Hash, idx)

		if !entry.XorHash.IsNull() {
			for it := hashList.Iter(idx); !it.IsDone(); it.Next() {
				xorHash := entry.XorHash
				xorHash.Xor(it.GetEntry().Hash)

				if current, ok := hashIndex.xorData[xorHash]; ok && hashIndex.nodes[current.tail].idx == idx {
					// Already present.
					continue
				}

				hashIndex.push(hashIndex.xorData, xorHash, idx)
			}
		}
	}
}

func (hashIndex *HashIndex) push(m map[Hash]chain, key Hash, idx int) {
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

// Lookup returns the entries whose hash is key.
func (hashIndex *HashIndex) Lookup(key Hash) ChainIter {
	return hashIndex.iter(hashIndex.data, key)
}

// LookupXor returns the entries which have a field that, when removed, makes their XorHash equal to key.
func (hashIndex *HashIndex) LookupXor(key Hash) ChainIter {
	return hashIndex.iter(hashIndex.xorData, key)
}

func (hashIndex *HashIndex) iter(m map[Hash]chain, key Hash) ChainIter {
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
