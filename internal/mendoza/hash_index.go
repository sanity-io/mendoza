package mendoza

type HashIndex struct {
	Data map[Hash][]int
}

func NewHashIndex(hashList *HashList) *HashIndex {
	hashIndex := &HashIndex{
		Data: map[Hash][]int{},
	}

	for idx, entry := range hashList.Entries {
		hashIndex.Data[entry.Hash] = append(hashIndex.Data[entry.Hash], idx)
	}

	return hashIndex
}
