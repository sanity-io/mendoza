package mendoza

import (
	"github.com/sanity-io/mendoza/internal/mendoza"
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

	reqs := []request{
		{
			initialContext: -1,
			size:           root.Size + 1,
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
	initialContext int
	size           int
	patch          Patch
	outputKey      string
}

func (d *differ) reconstruct(idx int, reqs []request) {
	if len(reqs) == 0 {
		return
	}

	// Note that the context is the object _above_ the target.
	// Example:
	// - Context is (left document): {"_type": "person", "name": "Bob"}
	// - Idx is (right document):    "Bob Bobson"
	// However, in many cases it's most interesting to compare against is the field with the same key.
	// We therefore first look at each request/context and find the "primary index".
	// This is the index (into the hash list).

	entry := d.right.Entries[idx]
	primaries := make([]int, 0, len(reqs))

	for _, req := range reqs {
		primaryIdx := -1

		if req.initialContext == -1 {
			primaryIdx = 0
		} else if entry.Reference.IsMapEntry() && d.left.IsNonEmptyMap(req.initialContext) {
			for it := d.left.Iter(req.initialContext); !it.IsDone(); it.Next() {
				key := it.GetKey()

				if key == entry.Reference.Key {
					// We found the same key
					primaryIdx = it.GetIndex()
					break
				}

				if key > entry.Reference.Key {
					// Since the keys are iterated in sorted order we can stop here.
					break
				}
			}
		}

		primaries = append(primaries, primaryIdx)
	}

	if d.right.IsNonEmptyMap(idx) {
		d.reconstructMap(idx, reqs, primaries)
		return
	}

	if d.right.IsNonEmptySlice(idx) {
		d.reconstructSlice(idx, reqs, primaries)
		return
	}
}

// Creates a patch which enters an entry in the left document.
func (d *differ) enterPatch(enter EnterType, idx int) Op {
	if idx == 0 {
		return OpEnterRoot{enter}
	}

	ref := d.left.Entries[idx].Reference

	if ref.IsMapEntry() {
		return OpEnterField{enter, ref.Index}
	}

	return OpEnterElement{enter, ref.Index}
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

type candidate interface {
	// Is this candidate missing an exact value for the (right) reference?
	IsMissing(reference mendoza.Reference) bool

	// Registers the index of the request so that we can later extract the result.
	RegisterRequest(childIdx int, childRef mendoza.Reference, reqIdx int)
}

func (d *differ) buildRequests(childReqs map[int][]request, contextIdx int, cand candidate) {
	for childIdx, reqs := range childReqs {
		child := d.right.Entries[childIdx]
		if cand.IsMissing(child.Reference) {
			req := request{
				initialContext: contextIdx,
				size:           child.Size + 1,
			}
			reqIdx := len(reqs)
			childReqs[childIdx] = append(reqs, req)
			cand.RegisterRequest(childIdx, child.Reference, reqIdx)
		}
	}
}

// This stores information about each candidate map in the left-side document.
type mapCandidate struct {
	alias            map[string]mapAlias
	seenKeys         map[string]struct{}
	childReqsMapping map[int]int
	requestIdx       int
}

type mapAlias struct {
	fieldIdx int
	sameKey  bool
}

func (mc *mapCandidate) init(requestIdx int) {
	mc.alias = make(map[string]mapAlias)
	mc.seenKeys = make(map[string]struct{})
	mc.childReqsMapping = make(map[int]int)
	mc.requestIdx = requestIdx
}

func (mc *mapCandidate) insertAlias(target mendoza.Reference, source mendoza.Reference) {
	mc.seenKeys[target.Key] = struct{}{}

	if target.Key == source.Key {
		mc.alias[target.Key] = mapAlias{
			fieldIdx: source.Index,
			sameKey:  true,
		}
		return
	}

	if current, ok := mc.alias[target.Key]; ok {
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
	mc.childReqsMapping[childIdx] = reqIdx
}

func (d *differ) reconstructMap(idx int, reqs []request, primaries []int) {
	// left-index -> mapCandidate
	candidates := map[int]mapCandidate{}

	// right-index -> list of requests
	fieldRequests := map[int][]request{}

	// left-index -> index inside reqs
	requestMapping := map[int]int{}
	for idx, req := range reqs {
		requestMapping[req.initialContext] = idx
	}

	// First search for possible common objects in different locations:
	for it := d.right.Iter(idx); !it.IsDone(); it.Next() {
		fieldEntry := it.GetEntry()
		fieldIdx := it.GetIndex()

		// Populate fieldRequests
		fieldRequests[fieldIdx] = nil

		for _, otherIdx := range d.hashIndex.Data[fieldEntry.Hash] {
			otherEntry := d.left.Entries[otherIdx]

			if otherEntry.Reference.IsMapEntry() {
				contextIdx := otherEntry.Parent
				contextEntry := d.left.Entries[contextIdx]

				// Note: This what only makes us consider siblings as candidates
				if reqIdx, ok := requestMapping[contextEntry.Parent]; ok {
					cand, ok := candidates[contextIdx]
					if !ok {
						cand.init(reqIdx)
					}
					cand.insertAlias(fieldEntry.Reference, otherEntry.Reference)
					candidates[contextIdx] = cand
				}
			}
		}
	}

	// TODO: Reduce the number of candidates to avoid exploring too much.

	// Also consider the primary elements
	for reqIdx, primaryIdx := range primaries {
		if primaryIdx != -1 {
			cand, ok := candidates[primaryIdx]
			if !ok {
				cand.init(reqIdx)
			}
			candidates[primaryIdx] = cand
		}
	}

	for contextIdx, cand := range candidates {
		d.buildRequests(fieldRequests, contextIdx, &cand)
	}

	for fieldIdx, reqs := range fieldRequests {
		d.reconstruct(fieldIdx, reqs)
	}

	for contextIdx, cand := range candidates {
		size := 0
		patch := Patch{}

		removeKeys := map[string]int{}

		for it := d.left.Iter(contextIdx); !it.IsDone(); it.Next() {
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

		patch = append(patch, d.enterPatch(enterMode, contextIdx))
		size += 2

		if enterMode == EnterCopy {
			// Delete fields we don't need
			for _, removeIdx := range removeKeys {
				patch = append(patch, OpObjectDeleteField{removeIdx})
				size += 2
			}
		}

		for target, alias := range cand.alias {
			if alias.sameKey {
				if enterMode == EnterBlank {
					// We only need this if we're starting with a blank object
					patch = append(patch, OpObjectCopyField{alias.fieldIdx})
					size += 2
				}
			} else {
				patch = append(patch, OpEnterField{EnterCopy, alias.fieldIdx}, OpReturnIntoObject{target})
				size += 2 + 1 + len(target)
			}
		}

		for fieldIdx, fieldRequestIdx := range cand.childReqsMapping {
			fieldEntry := d.right.Entries[fieldIdx]
			key := fieldEntry.Reference.Key
			req := fieldRequests[fieldIdx][fieldRequestIdx]

			if req.patch == nil {
				patch = append(patch, OpObjectSetFieldValue{key, fieldEntry.Value})
				size += 1 + len(key) + fieldEntry.Size
			} else {
				patch = append(patch, req.patch...)
				size += req.size

				if req.outputKey == key {
					patch = append(patch, OpReturnIntoObject{""})
					size += 1
				} else {
					patch = append(patch, OpReturnIntoObject{key})
					size += 1 + len(key)
				}
			}
		}

		req := &reqs[cand.requestIdx]

		if size < req.size {
			// Found a better thing!
			req.size = size
			req.patch = patch
			req.outputKey = d.left.Entries[contextIdx].Reference.Key
		}
	}
}

type sliceAlias struct {
	elementIdx     int
	prevIsAdjacent bool
	nextIsAdjacent bool
}

type sliceCandidate struct {
	alias            map[int]sliceAlias
	childReqsMapping map[int]int
	requestIdx       int
}

func (sc *sliceCandidate) init(reqIdx int) {
	sc.alias = map[int]sliceAlias{}
	sc.childReqsMapping = map[int]int{}
	sc.requestIdx = reqIdx
}

func (sc *sliceCandidate) IsMissing(reference mendoza.Reference) bool {
	_, ok := sc.alias[reference.Index]
	return !ok
}

func (sc *sliceCandidate) RegisterRequest(childIdx int, childRef mendoza.Reference, reqIdx int) {
	sc.childReqsMapping[childIdx] = reqIdx
}

func (sc *sliceCandidate) insertAlias(target mendoza.Reference, source mendoza.Reference) {
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

func (d *differ) reconstructSlice(idx int, reqs []request, primaries []int) {
	// left-index -> sliceCandidate
	candidates := map[int]sliceCandidate{}

	// right-index -> requests
	elementRequests := map[int][]request{}

	// left-index -> index inside reqs
	requestMapping := map[int]int{}
	for idx, req := range reqs {
		requestMapping[req.initialContext] = idx
	}

	for it := d.right.Iter(idx); !it.IsDone(); it.Next() {
		elementEntry := it.GetEntry()
		elementIdx := it.GetIndex()

		// Populate elementRequests
		elementRequests[elementIdx] = nil

		for _, otherIdx := range d.hashIndex.Data[elementEntry.Hash] {
			otherEntry := d.left.Entries[otherIdx]

			if otherEntry.Reference.IsSliceEntry() {
				scope := otherEntry.Parent
				scopeEntry := d.left.Entries[scope]

				// Note: This what only makes us only consider children of the contexts as candidates
				if reqIdx, ok := requestMapping[scopeEntry.Parent]; ok {
					cand, ok := candidates[scope]
					if !ok {
						cand.init(reqIdx)
					}
					cand.insertAlias(elementEntry.Reference, otherEntry.Reference)
					candidates[scope] = cand
				}
			}
		}
	}

	// TODO: Reduce number of candidates per request

	// Always consider the primary elements
	for reqIdx, primaryIdx := range primaries {
		if primaryIdx != -1 {
			cand, ok := candidates[primaryIdx]
			if !ok {
				cand.init(reqIdx)
			}
			candidates[primaryIdx] = cand
		}
	}

	for contextIdx, cand := range candidates {
		d.buildRequests(elementRequests, contextIdx, &cand)
	}

	for elementIdx, reqs := range elementRequests {
		d.reconstruct(elementIdx, reqs)
	}

	for contextIdx, cand := range candidates {
		size := 0
		patch := Patch{}

		patch = append(patch, d.enterPatch(EnterBlank, contextIdx))
		size += 2

		startSlice := -1

		for it := d.right.Iter(idx); !it.IsDone(); it.Next() {
			elementIdx := it.GetIndex()
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
				fieldRequestIdx := cand.childReqsMapping[elementIdx]
				req := elementRequests[elementIdx][fieldRequestIdx]
				if req.patch == nil {
					patch = append(patch, OpArrayAppendValue{elementEntry.Value})
					size += 1 + elementEntry.Size
				} else {
					patch = append(patch, req.patch...)
					size += req.size
					patch = append(patch, OpReturnIntoArray{})
					size += 1
				}
			}

			pos++
		}

		req := &reqs[cand.requestIdx]

		if size < req.size {
			// Found a better thing!
			req.size = size
			req.patch = patch
			req.outputKey = d.left.Entries[contextIdx].Reference.Key
		}
	}
}
