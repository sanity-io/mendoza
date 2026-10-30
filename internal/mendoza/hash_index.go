package mendoza

// HashIndex maps a hash to the ascending list of entry indices with that hash.
// Lists are stored as intrusive chains in one node slice, so building the
// index allocates nothing once the maps and slice have grown (it is reusable).
type HashIndex struct {
	XorData map[Hash]chain
	nodes   []node
}

type chain struct{ head, tail int32 }
type node struct{ idx, next int32 }

func NewHashIndex(hashList *HashList) *HashIndex {
	h := &HashIndex{}
	h.Build(hashList)
	return h
}

func push[K comparable](h *HashIndex, m map[K]chain, key K, idx int) {
	n := int32(len(h.nodes))
	h.nodes = append(h.nodes, node{idx: int32(idx), next: -1})
	if c, ok := m[key]; ok {
		h.nodes[c.tail].next = n
		c.tail = n
		m[key] = c
	} else {
		m[key] = chain{n, n}
	}
}

func (h *HashIndex) Build(hashList *HashList) {
	if h.XorData == nil {
		h.XorData = make(map[Hash]chain, len(hashList.Entries))
	} else {
		clear(h.XorData)
	}
	h.nodes = h.nodes[:0]

	for idx := range hashList.Entries {
		entry := &hashList.Entries[idx]

		if !entry.XorHash.IsNull() {
			for it := hashList.Iter(idx); !it.IsDone(); it.Next() {
				xorHash := entry.XorHash
				xorHash.Xor(hashList.Entries[it.idx].Hash)
				if c, ok := h.XorData[xorHash]; ok && h.nodes[c.tail].idx == int32(idx) {
					continue // already present
				}
				push(h, h.XorData, xorHash, idx)
			}
		}
	}
}

// ChainIter walks the entry indices for one hash in ascending order.
type ChainIter struct {
	nodes []node
	cur   int32
}

func (h *HashIndex) LookupXor(key Hash) ChainIter { return iter(h, h.XorData, key) }

func iter[K comparable](h *HashIndex, m map[K]chain, key K) ChainIter {
	if c, ok := m[key]; ok {
		return ChainIter{h.nodes, c.head}
	}
	return ChainIter{cur: -1}
}

func (it *ChainIter) Next() (int, bool) {
	if it.cur < 0 {
		return 0, false
	}
	n := it.nodes[it.cur]
	it.cur = n.next
	return int(n.idx), true
}
