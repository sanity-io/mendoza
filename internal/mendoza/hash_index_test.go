package mendoza

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestHashIndexIsKeyedByParent(t *testing.T) {
	// Every block has the same "_type" and "marks", so their hashes repeat under different parents.
	var doc interface{}
	err := json.Unmarshal([]byte(`{"body": [
		{"_type": "block", "children": [{"_type": "span", "marks": [], "text": "a"}, {"_type": "span", "marks": [], "text": "b"}]},
		{"_type": "block", "children": [{"_type": "span", "marks": [], "text": "c"}]}
	]}`), &doc)
	if err != nil {
		t.Fatal(err)
	}
	hashList, err := HashListFor(doc, nil)
	if err != nil {
		t.Fatal(err)
	}
	hashIndex := NewHashIndex(hashList)

	// Build the same index the obvious way to compare against.
	data := map[ChildKey][]int{}
	xorData := map[ChildKey][]int{}
	for idx, entry := range hashList.Entries {
		key := ChildKey{Parent: entry.Parent, Hash: entry.Hash}
		data[key] = append(data[key], idx)
		if entry.XorHash.IsNull() {
			continue
		}
		for it := hashList.Iter(idx); !it.IsDone(); it.Next() {
			xorKey := ChildKey{Parent: entry.Parent, Hash: entry.XorHash}
			xorKey.Hash.Xor(it.GetEntry().Hash)
			if current := xorData[xorKey]; len(current) == 0 || current[len(current)-1] != idx {
				xorData[xorKey] = append(current, idx)
			}
		}
	}

	if !reflect.DeepEqual(hashIndex.Data, data) {
		t.Errorf("Data = %v, want %v", hashIndex.Data, data)
	}
	if !reflect.DeepEqual(hashIndex.XorData, xorData) {
		t.Errorf("XorData = %v, want %v", hashIndex.XorData, xorData)
	}
}
