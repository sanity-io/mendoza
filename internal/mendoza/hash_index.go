package mendoza

type HashIndex struct {
	Data map[Hash][]int
	XorData map[Hash][]int
}

func NewHashIndex(hashList *HashList) *HashIndex {
	hashIndex := &HashIndex{
		Data: map[Hash][]int{},
		XorData: map[Hash][]int{},
	}

	for idx, entry := range hashList.Entries {
		hashIndex.Data[entry.Hash] = append(hashIndex.Data[entry.Hash], idx)

		if !entry.XorHash.IsNull() {
			for it := hashList.Iter(idx); !it.IsDone(); it.Next() {
				childEntry := it.GetEntry()

				xorHash := entry.XorHash
				xorHash.Xor(childEntry.Hash)

				current := hashIndex.XorData[xorHash]
				if len(current) > 0 && current[len(current) - 1] == idx {
					// Already present.
					continue
				}

				hashIndex.XorData[xorHash] = append(current, idx)
			}
		}
	}

	return hashIndex
}
