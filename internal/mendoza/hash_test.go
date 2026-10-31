package mendoza

import (
	"reflect"
	"testing"
)

var docs = []interface{}{
	nil,
	"hello",
	map[string]interface{}{"b": 1.0, "a": []interface{}{"x", "y", map[string]interface{}{}}, "c": true},
	[]interface{}{"x", "x", nil, false, map[string]interface{}{"a": "x", "b": "x"}},
	map[string]interface{}{"nested": map[string]interface{}{"z": "1", "y": map[string]interface{}{"x": []interface{}{}}}},
}

func TestHashesDistinguishTypes(t *testing.T) {
	values := []interface{}{
		nil, true, false, 0.0, 1.0, "", "1", "true",
		map[string]interface{}{}, []interface{}{},
		map[string]interface{}{"a": "b"}, map[string]interface{}{"b": "a"}, map[string]interface{}{"ab": ""},
		[]interface{}{"a", "b"}, []interface{}{"b", "a"}, []interface{}{"ab"},
	}
	seen := map[Hash]interface{}{}
	for _, value := range values {
		list, err := HashListFor(value, nil)
		if err != nil {
			t.Fatal(err)
		}
		hash := list.Entries[0].Hash
		if other, ok := seen[hash]; ok {
			t.Errorf("%#v and %#v have the same hash", value, other)
		}
		seen[hash] = value
	}
}

func TestFillMatchesHashListFor(t *testing.T) {
	// Fill the same list with each document in turn (and in reverse) to check that nothing leaks between them.
	var reused HashList
	for _, order := range [][]int{{0, 1, 2, 3, 4}, {4, 3, 2, 1, 0}} {
		for _, i := range order {
			fresh, err := HashListFor(docs[i], nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := reused.Fill(docs[i], nil); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(fresh.Entries, reused.Entries) {
				t.Errorf("doc %d: Fill gives different entries than HashListFor", i)
			}
		}
	}
}

func TestFillAfterError(t *testing.T) {
	var list HashList
	invalid := map[string]interface{}{"a": "x", "b": map[string]interface{}{"c": struct{}{}}}
	if err := list.Fill(invalid, nil); err == nil {
		t.Fatal("expected an error for an unsupported type")
	}
	if err := list.Fill(docs[2], nil); err != nil {
		t.Fatal(err)
	}
	if len(list.keys) != 0 {
		t.Errorf("key stack has %d stale keys after an error", len(list.keys))
	}
	fresh, err := HashListFor(docs[2], nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fresh.Entries, list.Entries) {
		t.Error("Fill after an error gives different entries than HashListFor")
	}
}

// naiveIndex builds the index the obvious way, to compare HashIndex against.
func naiveIndex(hashList *HashList) (map[Hash][]int, map[Hash][]int) {
	data := map[Hash][]int{}
	xorData := map[Hash][]int{}
	for idx, entry := range hashList.Entries {
		data[entry.Hash] = append(data[entry.Hash], idx)
		if entry.XorHash.IsNull() {
			continue
		}
		for it := hashList.Iter(idx); !it.IsDone(); it.Next() {
			xorHash := entry.XorHash
			xorHash.Xor(it.GetEntry().Hash)
			if current := xorData[xorHash]; len(current) == 0 || current[len(current)-1] != idx {
				xorData[xorHash] = append(current, idx)
			}
		}
	}
	return data, xorData
}

func collect(it ChainIter) []int {
	var result []int
	for {
		idx, ok := it.Next()
		if !ok {
			return result
		}
		result = append(result, idx)
	}
}

func TestHashIndexMatchesNaiveIndex(t *testing.T) {
	var index HashIndex
	for _, order := range [][]int{{0, 1, 2, 3, 4}, {4, 3, 2, 1, 0}} {
		for _, i := range order {
			list, err := HashListFor(docs[i], nil)
			if err != nil {
				t.Fatal(err)
			}
			index.Build(list)
			data, xorData := naiveIndex(list)

			if len(index.data) != len(data) || len(index.xorData) != len(xorData) {
				t.Errorf("doc %d: index has %d/%d hashes, want %d/%d",
					i, len(index.data), len(index.xorData), len(data), len(xorData))
			}
			for hash, want := range data {
				if got := collect(index.Lookup(hash)); !reflect.DeepEqual(got, want) {
					t.Errorf("doc %d: Lookup = %v, want %v", i, got, want)
				}
			}
			for hash, want := range xorData {
				if got := collect(index.LookupXor(hash)); !reflect.DeepEqual(got, want) {
					t.Errorf("doc %d: LookupXor = %v, want %v", i, got, want)
				}
			}
			if got := collect(index.Lookup(Hash{})); got != nil {
				t.Errorf("doc %d: Lookup of a missing hash = %v, want nothing", i, got)
			}
		}
	}
}
