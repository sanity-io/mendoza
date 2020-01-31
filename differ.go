package mendoza

import (
	"github.com/sanity-io/mendoza/internal/mendoza"
	"unicode/utf8"
)

type differ struct {
	left      *mendoza.HashList
	right     *mendoza.HashList
	hashIndex *mendoza.HashIndex
}

// Creates a patch which can be applied to the left document to produce the right document.
func CreatePatch(left, right interface{}) (Patch, error) {
	leftList, err := mendoza.HashListFor(left)
	if err != nil {
		return nil, err
	}
	rightList, err := mendoza.HashListFor(right)
	if err != nil {
		return nil, err
	}

	hashIndex := mendoza.NewHashIndex(leftList)
	differ := differ{
		left:      leftList,
		right:     rightList,
		hashIndex: hashIndex,
	}
	return differ.build(), nil
}

// Creates two patches: The first can be applied to the left document to produce the right document,
// the second can be applied to the right document to produce the left document.
func CreateDoublePatch(left, right interface{}) (Patch, Patch, error) {
	leftList, err := mendoza.HashListFor(left)
	if err != nil {
		return nil, nil, err
	}
	rightList, err := mendoza.HashListFor(right)
	if err != nil {
		return nil, nil, err
	}

	leftHashIndex := mendoza.NewHashIndex(leftList)
	leftDiffer := differ{
		left:      leftList,
		right:     rightList,
		hashIndex: leftHashIndex,
	}

	rightHashIndex := mendoza.NewHashIndex(rightList)
	rightDiffer := differ{
		left:      rightList,
		right:     leftList,
		hashIndex: rightHashIndex,
	}
	return leftDiffer.build(), rightDiffer.build(), nil
}

/*

The main function in the differ is `reconstruct` which takes two parameters.
The first is the target value: a reference to a value in the right-hand side.
The second parameter is a slice of _requests_. A request has an _initial
context_ which is a reference to a value in the left-hand side. The goal of
`reconstruct` is then, for each request, build a patch which converts the
_initial context_ into the target value.  Every request also keeps track of the
_best size_.

For arrays/objects this is done in a few steps:

1. First we look at the target (ignoring any context) and find other values that are good candidates.
   This is a cheap check which uses the global hash index and only looks for objects/arrays that
   shares some fields/elements with the target. This can only check for exact equality.

2. Then we do filtering of candidates based on the requests' context:
   - Currently we only care about candidates that are direct children of the context.
   - Per request we pick the N best candidates to explore. This is just an heuristic because we only know
     "how many fields/elements they have in common" and nothing about the size of representing the patch of
     the differences.

3. For each field/element in the target we can then build a list of requests (based on the candidates)
   and recurse into `reconstruct`. The more candidates we have in the previous step, the more requests
   we'll have in this step, and the time complexity will be higher.

4. At this point we know have full information about each candidate: All patches are calculated.
   We then just need to connect each candidate with a request and we're done!

*/

func (d *differ) build() Patch {
	root := d.right.Entries[0]

	if d.left.Entries[0].Hash == root.Hash {
		// Exact same value
		return Patch{OpEnterRoot{EnterCopy}}
	}

	reqs := []request{
		{
			contextIdx: -1,
			primaryIdx: 0,
			size:       root.Size + 1,
		},
	}

	d.reconstruct(0, reqs)

	req := reqs[0]

	if req.patch == nil {
		return Patch{OpEnterValue{root.Value}}
	}

	return req.patch
}

type request struct {
	contextIdx int
	primaryIdx int
	size       int
	patch      Patch
	outputKey  string
}

func (req *request) update(patch Patch, size int, outputKey string) {
	if size < req.size {
		req.patch = patch
		req.size = size
		req.outputKey = outputKey
	}
}

func (d *differ) reconstruct(idx int, reqs []request) {
	if len(reqs) == 0 {
		return
	}

	entry := d.right.Entries[idx]

	if entry.IsNonEmptyMap() {
		d.reconstructMap(idx, reqs)
		return
	}

	if entry.IsNonEmptySlice() {
		d.reconstructSlice(idx, reqs)
		return
	}

	if rightString, ok := entry.Value.(string); ok {
		d.reconstructString(idx, rightString, reqs)
		return
	}
}

// Creates a patch which enters an entry in the left document.
func (d *differ) enterPatch(enter EnterType, idx int) Op {
	if idx == 0 {
		return OpEnterRoot{enter}
	}

	entry := d.left.Entries[idx]
	parentEntry := d.left.Entries[entry.Parent]

	if parentEntry.IsNonEmptyMap() {
		return OpEnterField{enter, entry.Reference.Index}
	}

	return OpEnterElement{enter, entry.Reference.Index}
}

/*

Some notes about how we're building up calls from candidates:

An object has multiple fields: {"a": "…", "b": …, "c": …}, and we find candidates of objects that are similar.

Imagine that we found three candidates:
  - cand1 has {"a": "…", "b": …, "c": "different"}
  - cand2 has {"a": "…", "b": "different", "c": …}
  - cand3 has {"a": "…", "b": "different", "c": "different"}
  - cand4 has {"a": "…", "b": …, "c": "different2"}

The crucial thing here is that we only need 2 recursive `reconstruct` calls (because `a` is equal in all candidates),
but some of the calls might have multiple requests.

In this case the 2 recursive calls are as follows:

- Reconstruct "c" with requests: [cand1, cand3, cand4]
- Reconstruct "b" with requests: [cand2, cand3]

*/

// This stores information about each candidate map in the left-side document.
type mapCandidate struct {
	alias            map[string]mapAlias
	seenKeys         map[string]struct{}
	requestIdx       int
	contextIdx       int
}

type mapAlias struct {
	fieldIdx int
	sameKey  bool
}

func (mc *mapCandidate) init(contextIdx int, requestIdx int) {
	mc.alias = make(map[string]mapAlias)
	mc.seenKeys = make(map[string]struct{})
	mc.requestIdx = requestIdx
	mc.contextIdx = contextIdx
}

func (mc *mapCandidate) insertAlias(target mendoza.Reference, source mendoza.Reference, size int) {
	current, currentOk := mc.alias[target.Key]

	mc.seenKeys[target.Key] = struct{}{}

	if target.Key == source.Key {
		mc.alias[target.Key] = mapAlias{
			fieldIdx: source.Index,
			sameKey:  true,
		}
		return
	}

	if currentOk {
		if current.sameKey {
			// Prefer sameKey
			return
		}

		if current.fieldIdx < source.Index {
			// Prefer lowest fieldIdx to ensure consistent patches
			return
		}
	}

	mc.alias[target.Key] = mapAlias{
		fieldIdx: source.Index,
		sameKey:  false,
	}
}

func (mc *mapCandidate) IsMissing(reference mendoza.Reference) bool {
	_, ok := mc.alias[reference.Key]
	return !ok
}

func (mc *mapCandidate) RegisterRequest(childIdx int, childRef mendoza.Reference, reqIdx int) {
	mc.seenKeys[childRef.Key] = struct{}{}
}

func (d *differ) reconstructMap(idx int, reqs []request) {
	// right-index -> list of requests
	fieldRequests := [][]request{}

	// The input here is a list of requests. Each requests has _context_ and _primary_ which looks like this:
	//
	//   Left           Right
	//     |
	//   Context
	//     |
	//   Primary        Target
	//
	// The mandate of this method is to "assuming the program is currently in _context_; create a patch which
	// produces _target_". Note that _primary_ is just a hint for where this method could look for differences.
	// It's typically the field with same name, or an element in the same position. We can also assume that
	// _primary_ is different from _target_ (otherwise why would you ask for the differences?).
	//
	// Note that context can either be an array or an object.

	// Currently we're only looking at primary.

	candidates := make([]mapCandidate, 0, len(reqs))

	for i, req := range reqs {
		if !d.left.Entries[req.primaryIdx].IsNonEmptyMap() {
			continue
		}

		cand := mapCandidate{}
		cand.init(req.primaryIdx, i)
		candidates = append(candidates, cand)
	}

	for it := d.right.Iter(idx); !it.IsDone(); it.Next() {
		fieldEntry := it.GetEntry()
		fieldRequests = append(fieldRequests, nil)

		for _, otherIdx := range d.hashIndex.Data[fieldEntry.Hash] {
			otherEntry := d.left.Entries[otherIdx]

			for candIdx := range candidates {
				cand := &candidates[candIdx]
				if cand.contextIdx == otherEntry.Parent {
					cand.insertAlias(fieldEntry.Reference, otherEntry.Reference, fieldEntry.Size)
				}
			}
		}
	}

	// Now build the requests
	for _, cand := range candidates {
		contextIter := d.left.Iter(cand.contextIdx)

		i := 0
		for it := d.right.Iter(idx); !it.IsDone(); it.Next() {
			fieldEntry := it.GetEntry()
			fieldKey := fieldEntry.Reference.Key

			cand.seenKeys[fieldKey] = struct{}{}

			if _, ok := cand.alias[fieldKey]; !ok {
				for !contextIter.IsDone() {
					key := contextIter.GetKey()

					if key > fieldKey {
						break
					}

					if key == fieldKey {
						fieldReqs := fieldRequests[i]
						fieldReqs = append(fieldReqs, request{
							contextIdx: cand.contextIdx,
							primaryIdx: contextIter.GetIndex(),
							size:       fieldEntry.Size + 1,
						})
						fieldRequests[i] = fieldReqs
						break
					}

					contextIter.Next()
				}
			}
			i++
		}
	}

	for it := d.right.Iter(idx); !it.IsDone(); it.Next() {
		d.reconstruct(it.GetIndex(), fieldRequests[it.GetEntry().Reference.Index])
	}

	for _, cand := range candidates {
		primaryIdx := cand.contextIdx

		size := 0
		patch := Patch{}

		removeKeys := map[string]int{}

		for it := d.left.Iter(primaryIdx); !it.IsDone(); it.Next() {
			ref := it.GetEntry().Reference
			if _, ok := cand.seenKeys[ref.Key]; ok {
				// do nothing
			} else {
				removeKeys[ref.Key] = ref.Index
			}
		}

		removeCount := len(removeKeys)
		aliasCount := len(cand.alias)

		enterMode := EnterBlank

		if removeCount < aliasCount {
			// Note: This doesn't currently take into account that we have two types of aliasing.
			// It's shorter to alias a field with the same key (one single copy field).
			// For now let's assume that the difference here is pretty small either way.
			enterMode = EnterCopy
		}

		patch = append(patch, d.enterPatch(enterMode, primaryIdx))
		size += 2

		if enterMode == EnterCopy {
			// Delete fields we don't need
			for _, removeIdx := range removeKeys {
				patch = append(patch, OpObjectDeleteField{removeIdx})
				size += 2
			}
		}

		for it := d.right.Iter(idx); !it.IsDone(); it.Next() {
			fieldEntry := it.GetEntry()
			fieldKey := fieldEntry.Reference.Key

			if alias, ok := cand.alias[fieldKey]; ok {
				if alias.sameKey {
					if enterMode == EnterBlank {
						// We only need this if we're starting with a blank object
						patch = append(patch, OpObjectCopyField{alias.fieldIdx})
						size += 2
					}
				} else {
					patch = append(patch, OpEnterField{EnterCopy, alias.fieldIdx}, OpReturnIntoObject{fieldKey})
					size += 2 + 1 + len(fieldKey)
				}
			} else {
				didPatch := false

				// Not an alias. Look up the real diff.
				for _, fieldReq := range fieldRequests[fieldEntry.Reference.Index] {
					if fieldReq.contextIdx == cand.contextIdx {
						// Match it up with the correct context
						if fieldReq.patch != nil {
							patch = append(patch, fieldReq.patch...)
							size += fieldReq.size

							if fieldReq.outputKey == fieldKey {
								patch = append(patch, OpReturnIntoObjectKeyless{})
								size += 1
							} else {
								patch = append(patch, OpReturnIntoObject{fieldKey})
								size += 1 + len(fieldKey)
							}
							didPatch = true
						}

						break
					}
				}

				if !didPatch {
					patch = append(patch, OpObjectSetFieldValue{fieldKey, fieldEntry.Value})
					size += 1 + len(fieldKey) + fieldEntry.Size
				}
			}
		}


		req := &reqs[cand.requestIdx]
		req.update(patch, size, d.left.Entries[primaryIdx].Reference.Key)
	}
}

type sliceRequestData struct {
	candidates map[int]*sliceCandidate
}

type sliceAlias struct {
	elementIdx     int
	prevIsAdjacent bool
	nextIsAdjacent bool
}

type sliceCandidate struct {
	alias            map[int]sliceAlias
	requestIdx       int
	contextIdx       int
}

func (sc *sliceCandidate) init(contextIdx int, requestIdx int) {
	sc.alias = map[int]sliceAlias{}
	sc.requestIdx = requestIdx
	sc.contextIdx = contextIdx
}

func (sc *sliceCandidate) insertAlias(target mendoza.Reference, source mendoza.Reference, size int) {
	// We assume here that you'll only invoke this method in the same order
	// as you want to build the array.

	current, ok := sc.alias[target.Index]

	if ok && current.prevIsAdjacent {
		// Once we've found something which is adjacent. Don't look any further.
		return
	}

	if prevSource, prevOk := sc.alias[target.Index-1]; prevOk {
		if prevSource.elementIdx+1 == source.Index {
			// This one is perfectly adjacent. Use it!
			sc.alias[target.Index] = sliceAlias{
				elementIdx:     source.Index,
				prevIsAdjacent: true,
			}
			prevSource.nextIsAdjacent = true
			sc.alias[target.Index-1] = prevSource
			return
		}

		if source.Index <= prevSource.elementIdx {
			// We want to prefer values that are _after_ the previous index.

			if ok {
				// However, we can only return if we've already found a value.
				// Otherwise we must use this new value.
				return
			}
		}
	}

	if ok && current.elementIdx < source.Index {
		// Prefer smaller over larger
		return
	}

	sc.alias[target.Index] = sliceAlias{
		elementIdx:     source.Index,
		prevIsAdjacent: false,
	}
}

func (d *differ) reconstructSlice(idx int, reqs []request) {
	// right-index -> requests
	elementRequests := [][]request{}

	candidates := make([]sliceCandidate, 0, len(reqs))

	for i, req := range reqs {
		if !d.left.Entries[req.primaryIdx].IsNonEmptySlice() {
			continue
		}

		cand := sliceCandidate{}
		cand.init(req.primaryIdx, i)
		candidates = append(candidates, cand)
	}

	for it := d.right.Iter(idx); !it.IsDone(); it.Next() {
		elementEntry := it.GetEntry()
		elementRequests = append(elementRequests, nil)

		for _, otherIdx := range d.hashIndex.Data[elementEntry.Hash] {
			otherEntry := d.left.Entries[otherIdx]

			for candIdx := range candidates {
				cand := &candidates[candIdx]
				if cand.contextIdx == otherEntry.Parent {
					cand.insertAlias(elementEntry.Reference, otherEntry.Reference, elementEntry.Size)
				}
			}
		}
	}

	// Now build the requests
	for _, cand := range candidates {
		contextIter := d.left.Iter(cand.contextIdx)

		i := 0
		for it := d.right.Iter(idx); !it.IsDone(); it.Next() {
			if contextIter.IsDone() {
				break
			}

			elementEntry := it.GetEntry()

			if _, ok := cand.alias[elementEntry.Reference.Index]; !ok {
				elementReqs := elementRequests[i]
				elementReqs = append(elementReqs, request{
					contextIdx: cand.contextIdx,
					primaryIdx: contextIter.GetIndex(),
					size:       elementEntry.Size + 1,
				})
				elementRequests[i] = elementReqs

			}

			i++
			contextIter.Next()
		}
	}

	for it := d.right.Iter(idx); !it.IsDone(); it.Next() {
		d.reconstruct(it.GetIndex(), elementRequests[it.GetEntry().Reference.Index])
	}

	for _, cand := range candidates {
		contextIdx := cand.contextIdx
		size := 0
		patch := Patch{}

		patch = append(patch, d.enterPatch(EnterBlank, contextIdx))
		size += 2

		startSlice := -1

		for it := d.right.Iter(idx); !it.IsDone(); it.Next() {
			elementEntry := it.GetEntry()
			pos := elementEntry.Reference.Index

			if alias, ok := cand.alias[pos]; ok {
				if startSlice == -1 {
					startSlice = alias.elementIdx
				}

				if alias.nextIsAdjacent {
					// The next one is adjacent. We don't need to do anything!
				} else {
					patch = append(patch, OpArrayAppendSlice{startSlice, alias.elementIdx + 1})
					size += 3
					startSlice = -1
				}
			} else {
				didPatch := false

				for _, elementReq := range elementRequests[pos] {
					if elementReq.contextIdx == contextIdx {
						if elementReq.patch != nil {
							patch = append(patch, elementReq.patch...)
							size += elementReq.size
							patch = append(patch, OpReturnIntoArray{})
							size += 1

							didPatch = true
						}

						break
					}
				}

				if !didPatch {
					patch = append(patch, OpArrayAppendValue{elementEntry.Value})
					size += 1 + elementEntry.Size
				}
			}
		}

		req := &reqs[cand.requestIdx]
		req.update(patch, size, d.left.Entries[contextIdx].Reference.Key)
	}

}

// String handling

func commonPrefix(a, b string) int {
	i := 0
	for i < len(a) && i < len(b) {
		ar, size := utf8.DecodeRuneInString(a[i:])
		br, _ := utf8.DecodeRuneInString(b[i:])
		if ar != br {
			break
		}
		i += size
	}
	return i
}

func commonSuffix(a, b string, prefix int) int {
	i := 0
	for i < len(a)-prefix && i < len(b)-prefix {
		ar, size := utf8.DecodeLastRuneInString(a[:len(a)-i])
		br, _ := utf8.DecodeLastRuneInString(b[:len(b)-i])

		if ar != br {
			break
		}

		i += size
	}
	return i
}

func (d *differ) reconstructString(idx int, rightString string, reqs []request) {
	for reqIdx, req := range reqs {
		leftEntry := d.left.Entries[req.primaryIdx]

		leftString, ok := leftEntry.Value.(string)
		if !ok {
			continue
		}

		if leftString == rightString {
			panic("unnecessary reconstruction of string")
		}

		patch := Patch{}
		size := 0

		patch = append(patch, d.enterPatch(EnterBlank, req.primaryIdx))
		size += 2

		prefix := commonPrefix(leftString, rightString)

		if prefix > 0 {
			patch = append(patch, OpStringAppendSlice{0, prefix})
			size += 3
		}

		suffix := commonSuffix(leftString, rightString, prefix)

		mid := rightString[prefix : len(rightString)-suffix]
		if len(mid) > 0 {
			patch = append(patch, OpStringAppendString{mid})
			size += 1 + len(mid)
		}

		if suffix > 0 {
			patch = append(patch, OpStringAppendSlice{len(leftString) - suffix, len(leftString)})
			size += 3
		}

		req := &reqs[reqIdx]
		req.update(patch, size, d.left.Entries[req.primaryIdx].Reference.Key)
	}
}
