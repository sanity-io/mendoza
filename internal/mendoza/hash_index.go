package mendoza

// ChildKey identifies the children of Parent with a given hash.
type ChildKey struct {
	Parent int
	Hash   Hash
}

// HashIndex finds entries by hash under a given parent. Keying by parent keeps lookups proportional to the parent's
// children, rather than to every entry in the document that shares the hash (such as "_type": "block").
type HashIndex struct {
	Data    map[ChildKey][]int
	XorData map[ChildKey][]int
}

func NewHashIndex(hashList *HashList) *HashIndex {
	hashIndex := &HashIndex{
		Data:    map[ChildKey][]int{},
		XorData: map[ChildKey][]int{},
	}

	for idx, entry := range hashList.Entries {
		key := ChildKey{Parent: entry.Parent, Hash: entry.Hash}
		hashIndex.Data[key] = append(hashIndex.Data[key], idx)

		if !entry.XorHash.IsNull() {
			for it := hashList.Iter(idx); !it.IsDone(); it.Next() {
				childEntry := it.GetEntry()

				xorKey := ChildKey{Parent: entry.Parent, Hash: entry.XorHash}
				xorKey.Hash.Xor(childEntry.Hash)

				current := hashIndex.XorData[xorKey]
				if len(current) > 0 && current[len(current)-1] == idx {
					// Already present.
					continue
				}

				hashIndex.XorData[xorKey] = append(current, idx)
			}
		}
	}

	return hashIndex
}
