package mendoza

import (
	"fmt"
	"sort"
)

// HashList stores a document as a flat list of entries. Each entry contains a hash of its contents, allowing you
// to quickly find equivalent sub trees.
type HashList struct {
	Entries     []HashEntry
	convertFunc func(value interface{}) interface{}

	// keys is a stack of sorted map keys, reused across maps (and documents) to avoid allocating.
	keys []string
}

func HashListFor(doc interface{}, convertFunc func(value interface{}) interface{}) (*HashList, error) {
	hashList := &HashList{convertFunc: convertFunc}
	err := hashList.AddDocument(doc)
	if err != nil {
		return nil, err
	}
	return hashList, nil
}

// Fill replaces the contents of the list with doc, reusing the memory of the previous document.
func (hashList *HashList) Fill(doc interface{}, convertFunc func(value interface{}) interface{}) error {
	hashList.Reset()
	hashList.convertFunc = convertFunc
	return hashList.AddDocument(doc)
}

// Reset empties the list, keeping its memory but dropping all references to the previous document.
func (hashList *HashList) Reset() {
	for i := range hashList.Entries {
		hashList.Entries[i] = HashEntry{}
	}
	hashList.Entries = hashList.Entries[:0]
	hashList.convertFunc = nil

	// The stack is normally empty already, but not if the previous document failed with an error.
	hashList.popKeys(0)
}

// pushSortedKeys pushes the keys of m onto the key stack in sorted order, and returns where they start. Index into
// the stack rather than holding a sub-slice, since pushes for nested maps may reallocate it.
func (hashList *HashList) pushSortedKeys(m map[string]interface{}) int {
	start := len(hashList.keys)
	for key := range m {
		hashList.keys = append(hashList.keys, key)
	}
	sort.Strings(hashList.keys[start:])
	return start
}

// popKeys removes the keys from start onwards, clearing them so the stack doesn't keep the document alive.
func (hashList *HashList) popKeys(start int) {
	for i := start; i < len(hashList.keys); i++ {
		hashList.keys[i] = ""
	}
	hashList.keys = hashList.keys[:start]
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

// Validate returns the error AddDocument would return for doc without a convert function, without hashing it. When
// doc has several unsupported values, the error may name a different one.
func Validate(doc interface{}) error {
	switch doc := doc.(type) {
	case nil, bool, float64, string:
		return nil
	case map[string]interface{}:
		for _, value := range doc {
			if err := Validate(value); err != nil {
				return err
			}
		}
		return nil
	case []interface{}:
		for _, value := range doc {
			if err := Validate(value); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported type: %T", doc)
	}
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
		keysStart := hashList.pushSortedKeys(obj)

		prevIdx := -1

		for idx := 0; idx < len(obj); idx++ {
			key := hashList.keys[keysStart+idx]
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

		hashList.popKeys(keysStart)

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

// Iter returns an iterator over the children of the entry idx, which must be a non-empty map or slice.
func (hashList *HashList) Iter(idx int) Iter {
	return Iter{
		hashList: hashList,
		idx:      idx + 1,
	}
}

// Iter iterates over the children of a map (in key order) or slice. It is a value so it can be stored without
// allocating.
type Iter struct {
	hashList *HashList
	idx      int
}

// GetIndex returns the entry index of the current child.
func (it *Iter) GetIndex() int {
	return it.idx
}

// GetEntry returns the current child's entry. The pointer is valid until the list is filled or reset again.
func (it *Iter) GetEntry() *HashEntry {
	return &it.hashList.Entries[it.idx]
}

// GetKey returns the current child's key. It is empty for slice elements.
func (it *Iter) GetKey() string {
	return it.GetEntry().Reference.Key
}

// IsDone reports whether the iterator has moved past the last child.
func (it *Iter) IsDone() bool {
	return it.idx == -1
}

// Next moves to the next child.
func (it *Iter) Next() {
	it.idx = it.GetEntry().Sibling
}
