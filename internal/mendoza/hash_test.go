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
func naiveIndex(hashList *HashList) (map[childKey][]int, map[childKey][]int) {
	children := map[childKey][]int{}
	xorData := map[childKey][]int{}
	for idx, entry := range hashList.Entries {
		key := childKey{parent: entry.Parent, hash: entry.Hash}
		children[key] = append(children[key], idx)
		if entry.XorHash.IsNull() {
			continue
		}
		for it := hashList.Iter(idx); !it.IsDone(); it.Next() {
			xorKey := childKey{parent: entry.Parent, hash: entry.XorHash}
			xorKey.hash.Xor(it.GetEntry().Hash)
			if current := xorData[xorKey]; len(current) == 0 || current[len(current)-1] != idx {
				xorData[xorKey] = append(current, idx)
			}
		}
	}
	return children, xorData
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
			children, xorData := naiveIndex(list)

			if len(index.children) != len(children) || len(index.xorData) != len(xorData) {
				t.Errorf("doc %d: index has %d/%d keys, want %d/%d",
					i, len(index.children), len(index.xorData), len(children), len(xorData))
			}
			for key, want := range children {
				if got := collect(index.LookupChild(key.parent, key.hash)); !reflect.DeepEqual(got, want) {
					t.Errorf("doc %d: LookupChild(%d) = %v, want %v", i, key.parent, got, want)
				}
			}
			for key, want := range xorData {
				if got := collect(index.LookupXor(key.parent, key.hash)); !reflect.DeepEqual(got, want) {
					t.Errorf("doc %d: LookupXor(%d) = %v, want %v", i, key.parent, got, want)
				}
				if got := collect(index.LookupXor(key.parent+len(list.Entries), key.hash)); got != nil {
					t.Errorf("doc %d: LookupXor under a missing parent = %v, want nothing", i, got)
				}
			}
			if got := collect(index.LookupChild(0, Hash{})); got != nil {
				t.Errorf("doc %d: LookupChild of a missing hash = %v, want nothing", i, got)
			}
			// A matching hash under another parent must not be returned.
			for key := range children {
				if got := collect(index.LookupChild(key.parent+len(list.Entries), key.hash)); got != nil {
					t.Errorf("doc %d: LookupChild under a missing parent = %v, want nothing", i, got)
				}
			}
		}
	}
}
