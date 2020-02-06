package mendoza

import (
	"fmt"
	"sort"
)

// HashList stores a document as a flat list of entries. Each entry contains a hash of its contents, allowing you
// to quickly find equivalent sub trees.
type HashList struct {
	Entries []HashEntry
	convertFunc func(value interface{}) interface{}
}

func HashListFor(doc interface{}, convertFunc func(value interface{}) interface{}) (*HashList, error) {
	hashList := &HashList{convertFunc: convertFunc}
	err := hashList.AddDocument(doc)
	if err != nil {
		return nil, err
	}
	return hashList, nil
}

type Reference struct {
	Index int
	Key   string
}

func MapEntryReference(idx int, key string) Reference {
	return Reference{Index: idx, Key: key}
}

func SliceEntryReference(idx int) Reference {
	return Reference{Index: idx}
}

type HashEntry struct {
	Hash      Hash
	XorHash   Hash
	Value     interface{}
	Size      int
	Parent    int
	Sibling   int
	Reference Reference
}

func (entry *HashEntry) IsNonEmptyMap() bool {
	val, ok := entry.Value.(map[string]interface{})
	return ok && len(val) > 0
}

func (entry *HashEntry) IsNonEmptySlice() bool {
	val, ok := entry.Value.([]interface{})
	return ok && len(val) > 0
}

func (hashList *HashList) AddDocument(obj interface{}) error {
	_, _, err := hashList.process(-1, Reference{}, obj)
	return err
}

func (hashList *HashList) process(parent int, ref Reference, obj interface{}) (result Hash, size int, err error) {
	current := len(hashList.Entries)

	var xorHash Hash

	if hashList.convertFunc != nil {
		obj = hashList.convertFunc(obj)
	}

	hashList.Entries = append(hashList.Entries, HashEntry{
		Parent:    parent,
		Value:     obj,
		Reference: ref,
		Sibling:   -1,
	})

	switch obj := obj.(type) {
	case nil:
		result = HashNull
		size = 1
	case bool:
		if obj {
			result = HashTrue
		} else {
			result = HashFalse
		}
		size = 1
	case float64:
		result = HashFloat64(obj)
		size = 8
	case string:
		result = HashString(obj)
		size = len(obj) + 1
	case map[string]interface{}:
		hasher := HasherMap
		keys := sortedKeys(obj)

		prevIdx := -1

		for idx, key := range keys {
			value := obj[key]
			entryIdx := len(hashList.Entries)
			valueHash, valueSize, err := hashList.process(current, MapEntryReference(idx, key), value)
			if err != nil {
				return result, size, err
			}

			size += len(key) + valueSize + 1

			if prevIdx != -1 {
				prevEntry := &hashList.Entries[prevIdx]
				prevEntry.Sibling = entryIdx
			}

			prevIdx = entryIdx

			hasher.WriteField(key, valueHash)
			xorHash.Xor(valueHash)
		}

		result = hasher.Sum()
	case []interface{}:
		hasher := HasherSlice

		prevIdx := -1

		for idx, value := range obj {
			entryIdx := len(hashList.Entries)

			valueHash, valueSize, err := hashList.process(current, SliceEntryReference(idx), value)
			if err != nil {
				return result, size, err
			}

			size += valueSize + 1

			if prevIdx != -1 {
				prevEntry := &hashList.Entries[prevIdx]
				prevEntry.Sibling = entryIdx
			}

			prevIdx = entryIdx

			hasher.WriteElement(valueHash)
		}

		result = hasher.Sum()
	default:
		return result, size, fmt.Errorf("unsupported type: %T", obj)
	}

	entry := &hashList.Entries[current]
	entry.Hash = result
	entry.Size = size
	entry.XorHash = xorHash

	return result, size, nil
}

func (hashList *HashList) Iter(idx int) *Iter {
	return &Iter{
		hashList: hashList,
		idx:      idx + 1,
	}
}

type Iter struct {
	hashList *HashList
	idx      int
}

func (it *Iter) GetIndex() int {
	return it.idx
}

func (it *Iter) GetEntry() HashEntry {
	return it.hashList.Entries[it.idx]
}

func (it *Iter) GetKey() string {
	return it.GetEntry().Reference.Key
}

func (it *Iter) IsDone() bool {
	return it.idx == -1
}

func (it *Iter) Next() {
	it.idx = it.GetEntry().Sibling
}

func sortedKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
