package mendoza

import (
	"unicode/utf8"

	"github.com/sanity-io/mendoza/internal/mendoza"
)

// Differ creates patches while reusing its working memory. On Go 1.24+ with the default runtime, warmed-up
// diffs can run without allocating. Older Go map implementations may still allocate when rebuilding the index.
// Writers and conversion functions may allocate independently.
//
// The zero value is ready to use. A Differ must not be copied after first use and is not safe for concurrent use.
// It keeps references to its last documents until the next diff, and retains buffer capacity for reuse.
// Inputs must not be mutated between diffing and writing the result.
type Differ struct {
	options             Options
	left, right         mendoza.HashList
	hashIndex           mendoza.HashIndex
	differ              differ
	patch, reversePatch span
}

// NewDiffer creates a Differ which uses the default options.
func NewDiffer() *Differ {
	return DefaultOptions.NewDiffer()
}

// NewDiffer creates a Differ which uses the given options.
func (options Options) NewDiffer() *Differ {
	return &Differ{options: options}
}

// Creates a patch which can be applied to the left document to produce the right document.
//
// This function uses the default options.
func CreatePatch(left, right interface{}) (Patch, error) {
	return DefaultOptions.CreatePatch(left, right)
}

// Creates two patches: The first can be applied to the left document to produce the right document,
// the second can be applied to the right document to produce the left document.
//
// This function uses the default options.
func CreateDoublePatch(left, right interface{}) (Patch, Patch, error) {
	return DefaultOptions.CreateDoublePatch(left, right)
}

// Creates a patch which can be applied to the left document to produce the right document.
//
// Use a Differ instead to reuse memory between calls.
//
// This intentionally creates a new Differ per call rather than pooling them: a pooled Differ would retain the
// buffers of the largest document it has seen. Callers who want reuse can hold a Differ themselves.
func (options *Options) CreatePatch(left, right interface{}) (Patch, error) {
	// No working memory is needed when there is no source document.
	if left == nil {
		if right == nil {
			return Patch{}, nil
		}
		return Patch{&OpValue{Value: right}}, nil
	}
	return options.NewDiffer().CreatePatch(left, right)
}

// Creates two patches: The first can be applied to the left document to produce the right document,
// the second can be applied to the right document to produce the left document.
//
// Use a Differ instead to reuse memory between calls.
func (options *Options) CreateDoublePatch(left, right interface{}) (Patch, Patch, error) {
	// Preserve the literal-only fast paths without allocating a Differ or tape.
	if left == nil && right == nil {
		return Patch{}, Patch{}, nil
	}
	if left == nil || right == nil {
		return Patch{&OpValue{Value: right}}, Patch{&OpValue{Value: left}}, nil
	}
	return options.NewDiffer().CreateDoublePatch(left, right)
}

// CreatePatch is like Options.CreatePatch, but reuses the Differ's working memory. The returned Patch and its
// operations are allocated separately and remain valid across calls. Values in the patch are not deep-copied.
func (d *Differ) CreatePatch(left, right interface{}) (Patch, error) {
	if err := d.Diff(left, right); err != nil {
		return nil, err
	}
	return d.differ.patchFor(d.patch), nil
}

// CreateDoublePatch is like Options.CreateDoublePatch, but reuses the Differ's working memory. The returned
// Patches and their operations are allocated separately and remain valid across calls. Values are not deep-copied.
func (d *Differ) CreateDoublePatch(left, right interface{}) (Patch, Patch, error) {
	if err := d.DiffDouble(left, right); err != nil {
		return nil, nil, err
	}
	return d.differ.patchFor(d.patch), d.differ.patchFor(d.reversePatch), nil
}

// Diff computes a patch which can be applied to the left document to produce the right document. Use WriteTo to
// write it out without allocating a Patch. The result is valid until the next diff (including CreatePatch or
// CreateDoublePatch). Each diff discards both previous results, even if it returns an error.
func (d *Differ) Diff(left, right interface{}) error {
	d.reset()

	if left == nil {
		if right != nil {
			d.patch = d.differ.valueSpan(right)
		}
		return nil
	}

	if changed, err := d.fill(left, right); !changed {
		return err
	}

	d.patch = d.build(&d.left, &d.right)
	return nil
}

// DiffDouble computes two patches: The first can be applied to the left document to produce the right document,
// the second can be applied to the right document to produce the left document. Use WriteTo and WriteReverseTo to
// write them out without allocating Patches. The results have the same lifetime as the result of Diff.
func (d *Differ) DiffDouble(left, right interface{}) error {
	d.reset()

	if left == nil && right == nil {
		return nil
	}

	if left == nil || right == nil {
		d.patch = d.differ.valueSpan(right)
		d.reversePatch = d.differ.valueSpan(left)
		return nil
	}

	if changed, err := d.fill(left, right); !changed {
		return err
	}

	d.patch = d.build(&d.left, &d.right)
	d.reversePatch = d.build(&d.right, &d.left)
	return nil
}

// WriteTo writes the last forward patch. It can be called repeatedly, including after a writer error.
// Before a diff or after a failed diff, it writes nothing.
func (d *Differ) WriteTo(w Writer) error {
	return d.differ.writeSpan(w, d.patch)
}

// WriteReverseTo writes the last reverse patch. It can be called repeatedly, including after a writer error.
// Before DiffDouble, after a forward-only diff, or after a failed diff, it writes nothing.
func (d *Differ) WriteReverseTo(w Writer) error {
	return d.differ.writeSpan(w, d.reversePatch)
}

// fill hashes both documents and reports whether they differ.
func (d *Differ) fill(left, right interface{}) (changed bool, err error) {
	if err := d.left.Fill(left, d.options.convertFunc); err != nil {
		return false, err
	}
	if err := d.right.Fill(right, d.options.convertFunc); err != nil {
		return false, err
	}
	return d.left.Entries[0].Hash != d.right.Entries[0].Hash, nil
}

// build indexes left and records a patch which turns it into right.
func (d *Differ) build(left, right *mendoza.HashList) span {
	d.hashIndex.Build(left)
	return d.differ.build(left, right, &d.hashIndex)
}

func (d *Differ) reset() {
	d.left.Reset()
	d.right.Reset()
	d.differ.reset()
	d.patch = span{}
	d.reversePatch = span{}
}

/*

The main function in the differ is `reconstruct` which takes two parameters.
The first is the target value: a reference to a value in the right-hand side.
The second parameter identifies a range of _requests_ in d.reqs. A request has an _initial
context_ which is a reference to a value in the left-hand side. The goal of
`reconstruct` is then, for each request, build a patch which converts the
_initial context_ into the target value.  Every request also keeps track of the
_best size_.

For arrays/objects this is done in a few steps:

1. Collect candidates for each request. We try its primary value and, for objects, other direct children
   of its context found through the xor index. Candidate discovery is a heuristic; patch costs are not
   known until reconstruction finishes.

2. Find exact aliases: fields/elements in each candidate whose value hashes match those in the target.

3. For each field/element in the target we can then build a list of requests (based on the candidates)
   and recurse into `reconstruct`. The more candidates we have in the previous step, the more requests
   we'll have in this step, and the time complexity will be higher.

4. At this point we have full information about each candidate: All patches are calculated.
   We then just need to connect each candidate with a request and we're done!

Memory: Requests, candidates and aliases are appended to reusable slices owned by the differ (arenas).
The slices grow as needed, and each reconstruction call truncates its scratch data before returning.
Use indices across recursive calls: appends can reallocate the arenas, leaving pointers into old backing
arrays stale. Left/right HashList entries are stable during reconstruction.

Patches are recorded as ops on a tape. Rejected candidates' newly emitted records are discarded, while selected
patches survive recursive calls so a parent can include a child's patch by span (codeInclude) instead of copying
it. Patch sizes are heuristic costs, not operation counts or exact serialized byte lengths.

*/

// span refers to the range [start, end) in one of the differ's slices.
type span struct {
	start, end int
}

type differ struct {
	left      *mendoza.HashList
	right     *mendoza.HashList
	hashIndex *mendoza.HashIndex

	tape []opRec

	reqs []request
	// requestOffsets records where each field/element's requests start in reqs; see requestsForField.
	requestOffsets []int

	candidates   []candidate
	mapAliases   []mapAlias
	sliceAliases []sliceAlias
}

// scratchMark records the lengths of the scratch arenas, so a reconstruction can remove what it appended.
type scratchMark struct {
	reqs, requestOffsets, candidates, mapAliases, sliceAliases int
}

func (d *differ) mark() scratchMark {
	return scratchMark{len(d.reqs), len(d.requestOffsets), len(d.candidates), len(d.mapAliases), len(d.sliceAliases)}
}

// restore truncates the scratch arenas to m. The tape is not restored: parents still need the patches on it.
func (d *differ) restore(m scratchMark) {
	d.reqs = d.reqs[:m.reqs]
	d.requestOffsets = d.requestOffsets[:m.requestOffsets]
	d.candidates = d.candidates[:m.candidates]
	d.mapAliases = d.mapAliases[:m.mapAliases]
	d.sliceAliases = d.sliceAliases[:m.sliceAliases]
}

func (d *differ) reset() {
	d.truncateTape(0)
	d.restore(scratchMark{})
}

func (d *differ) build(left, right *mendoza.HashList, hashIndex *mendoza.HashIndex) span {
	d.left = left
	d.right = right
	d.hashIndex = hashIndex

	root := &d.right.Entries[0]

	d.reqs = append(d.reqs[:0], request{
		contextIdx: -1,
		primaryIdx: 0,
		size:       root.Size + 1,
	})

	tapeStart := len(d.tape)
	d.reconstruct(0, span{0, 1})

	req := d.reqs[0]
	d.reqs = d.reqs[:0]

	if !req.hasPatch {
		// No reconstructed child is reachable when the whole document is replaced.
		// Keep any earlier build's tape (the forward patch in DiffDouble).
		d.truncateTape(tapeStart)
		return d.valueSpan(root.Value)
	}

	return req.patch
}

// Requests contain only indices, not document references, so their arena can be reused without clearing it.
type request struct {
	// Both indices refer to the left document. contextIdx is -1 for the root request.
	contextIdx int
	primaryIdx int
	size       int
	patch      span
	hasPatch   bool
	// patchSourceIdx is the left-document entry used by the chosen patch, whose key is needed on return.
	patchSourceIdx int
}

// updateRequest keeps the newly recorded patch only if it improves the request's cost.
// The patch must be at the end of the tape: rejected records have no users and can be discarded,
// while earlier child spans and previously selected patches must remain valid.
func (d *differ) updateRequest(reqIdx int, patch span, size int, patchSourceIdx int) {
	req := &d.reqs[reqIdx]
	if size >= req.size {
		d.truncateTape(patch.start)
		return
	}
	req.patch = patch
	req.hasPatch = true
	req.size = size
	req.patchSourceIdx = patchSourceIdx
}

// requestsForField returns the request range for a field or element. offset is the start of this container's
// entries in requestOffsets. Each container records one offset per field/element, plus a final end offset.
func (d *differ) requestsForField(offset int, field int) span {
	return span{d.requestOffsets[offset+field], d.requestOffsets[offset+field+1]}
}

// childPatch returns the first request in reqs for contextIdx if it has a patch, and nil otherwise. A later request
// with the same context must not override the first one. The pointer is only valid until the next append to d.reqs.
func (d *differ) childPatch(reqs span, contextIdx int) *request {
	for i := reqs.start; i < reqs.end; i++ {
		if d.reqs[i].contextIdx != contextIdx {
			continue
		}
		if req := &d.reqs[i]; req.hasPatch {
			return req
		}
		return nil
	}
	return nil
}

// reconstructChildren reconstructs each child of the target idx for the requests built for it at offset.
func (d *differ) reconstructChildren(idx int, offset int) {
	for it := d.right.Iter(idx); !it.IsDone(); it.Next() {
		d.reconstruct(it.GetIndex(), d.requestsForField(offset, it.GetEntry().Reference.Index))
	}
}

// findAliases records, for each candidate, which of its children equal a child of the target idx.
func (d *differ) findAliases(idx int, candidates span) {
	isMap := d.right.Entries[idx].IsNonEmptyMap()
	for it := d.right.Iter(idx); !it.IsDone(); it.Next() {
		entry := it.GetEntry()
		others := d.hashIndex.Lookup(entry.Hash)
		for otherIdx, ok := others.Next(); ok; otherIdx, ok = others.Next() {
			other := &d.left.Entries[otherIdx]
			for c := candidates.start; c < candidates.end; c++ {
				cand := &d.candidates[c]
				if cand.contextIdx != other.Parent {
					continue
				}
				if isMap {
					d.insertMapAlias(cand, entry.Reference, other.Reference)
				} else {
					d.insertSliceAlias(cand, entry.Reference, other.Reference)
				}
			}
		}
	}
}

func (d *differ) reconstruct(idx int, reqs span) {
	if reqs.start == reqs.end {
		return
	}

	entry := &d.right.Entries[idx]

	if entry.IsNonEmptyMap() {
		d.reconstructMap(idx, reqs)
		return
	}

	if entry.IsNonEmptySlice() {
		d.reconstructSlice(idx, reqs)
		return
	}

	if rightString, ok := entry.Value.(string); ok {
		d.reconstructString(rightString, reqs)
		return
	}
}

func (d *differ) enterBlank(idx int) {
	if idx == 0 {
		d.emit(opRec{code: codeBlank})
		return
	}

	entry := &d.left.Entries[idx]
	parentEntry := &d.left.Entries[entry.Parent]

	if parentEntry.IsNonEmptyMap() {
		d.emit(opRec{code: codePushFieldBlank, a: entry.Reference.Index})
	} else {
		d.emit(opRec{code: codePushElementBlank, a: entry.Reference.Index})
	}
}

func (d *differ) enterCopy(idx int) {
	if idx == 0 {
		// Root => Already on the stack.
		return
	}

	entry := &d.left.Entries[idx]
	parentEntry := &d.left.Entries[entry.Parent]

	if parentEntry.IsNonEmptyMap() {
		d.emit(opRec{code: codePushFieldCopy, a: entry.Reference.Index})
	} else {
		d.emit(opRec{code: codePushElementCopy, a: entry.Reference.Index})
	}
}

/*

Some notes about how we're building up calls from candidates:

An object has multiple fields: {"a": "…", "b": …, "c": …}, and we find candidates of objects that are similar.

Imagine that we found four candidates:
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

// This stores information about each candidate map/slice in the left-side document.
type candidate struct {
	contextIdx int // Candidate container in the left document.
	requestIdx int // Request in d.reqs that this candidate is trying to satisfy.
	// aliasOffset starts this candidate's range in mapAliases or sliceAliases, one slot per target field/element.
	aliasOffset int
	// aliasCount is the number of map alias slots which are set (maps only).
	aliasCount int
	// contextIter walks the candidate's fields/elements while the requests are built.
	contextIter mendoza.Iter
}

func (d *differ) addCandidate(contextIdx int, requestIdx int, aliasOffset int) {
	d.candidates = append(d.candidates, candidate{
		contextIdx:  contextIdx,
		requestIdx:  requestIdx,
		aliasOffset: aliasOffset,
		contextIter: d.left.Iter(contextIdx),
	})
}

type mapAlias struct {
	ok       bool
	fieldIdx int
	sameKey  bool
}

func (d *differ) addMapCandidate(contextIdx int, requestIdx int, fieldCount int) {
	d.addCandidate(contextIdx, requestIdx, len(d.mapAliases))
	for i := 0; i < fieldCount; i++ {
		d.mapAliases = append(d.mapAliases, mapAlias{})
	}
}

func (d *differ) mapAlias(cand *candidate, target mendoza.Reference) *mapAlias {
	return &d.mapAliases[cand.aliasOffset+target.Index]
}

func (d *differ) insertMapAlias(cand *candidate, target mendoza.Reference, source mendoza.Reference) {
	current := d.mapAlias(cand, target)

	// Every path below sets an unset slot.
	if !current.ok {
		cand.aliasCount++
	}

	if target.Key == source.Key {
		*current = mapAlias{
			ok:       true,
			fieldIdx: source.Index,
			sameKey:  true,
		}
		return
	}

	if current.ok {
		if current.sameKey {
			// Prefer sameKey
			return
		}

		if current.fieldIdx < source.Index {
			// Prefer lowest fieldIdx to ensure consistent patches
			return
		}
	}

	*current = mapAlias{
		ok:       true,
		fieldIdx: source.Index,
		sameKey:  false,
	}
}

// removedFields counts the fields in the left map leftIdx which don't exist in the right map rightIdx. If emit is
// set, it also emits an op deleting each of them (in key order).
func (d *differ) removedFields(leftIdx int, rightIdx int, emit bool) int {
	count := 0
	rightIter := d.right.Iter(rightIdx)
	for leftIter := d.left.Iter(leftIdx); !leftIter.IsDone(); leftIter.Next() {
		key := leftIter.GetKey()
		for !rightIter.IsDone() && rightIter.GetKey() < key {
			rightIter.Next()
		}
		if rightIter.IsDone() || rightIter.GetKey() != key {
			count++
			if emit {
				d.emit(opRec{code: codeObjectDeleteField, a: leftIter.GetEntry().Reference.Index})
			}
		}
	}
	return count
}

func (d *differ) reconstructMap(idx int, reqs span) {
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

	// Everything appended to the scratch arenas is removed again before returning.
	mark := d.mark()

	candidates := d.findMapCandidates(idx, reqs)
	d.findAliases(idx, candidates)
	offset := d.buildMapRequests(idx, candidates)
	d.reconstructChildren(idx, offset)
	for c := candidates.start; c < candidates.end; c++ {
		d.assembleMapPatch(idx, &d.candidates[c], offset)
	}

	d.restore(mark)
}

// findMapCandidates adds a candidate for each request whose primary value is a map, and for each sibling of the
// primary value that matches the target idx after removing one field's value hash (found through the xor index).
func (d *differ) findMapCandidates(idx int, reqs span) span {
	entry := &d.right.Entries[idx]
	fieldCount := len(entry.Value.(map[string]interface{}))
	start := len(d.candidates)

	for i := reqs.start; i < reqs.end; i++ {
		if primaryIdx := d.reqs[i].primaryIdx; d.left.Entries[primaryIdx].IsNonEmptyMap() {
			d.addMapCandidate(primaryIdx, i, fieldCount)
		}
	}

	for it := d.right.Iter(idx); !it.IsDone(); it.Next() {
		xorHash := entry.XorHash
		xorHash.Xor(it.GetEntry().Hash)

		others := d.hashIndex.LookupXor(xorHash)
		for otherIdx, ok := others.Next(); ok; otherIdx, ok = others.Next() {
			parent := d.left.Entries[otherIdx].Parent
			for i := reqs.start; i < reqs.end; i++ {
				if req := &d.reqs[i]; parent == req.contextIdx && otherIdx != req.primaryIdx {
					d.addMapCandidate(otherIdx, i, fieldCount)
				}
			}
		}
	}

	return span{start, len(d.candidates)}
}

// buildMapRequests requests a patch for each target field from each candidate's field with the same key, unless the
// candidate already has an alias for it. The requests are grouped by field; it returns their offset.
func (d *differ) buildMapRequests(idx int, candidates span) int {
	offset := len(d.requestOffsets)
	for it := d.right.Iter(idx); !it.IsDone(); it.Next() {
		fieldEntry := it.GetEntry()
		fieldKey := fieldEntry.Reference.Key
		d.requestOffsets = append(d.requestOffsets, len(d.reqs))

		for c := candidates.start; c < candidates.end; c++ {
			cand := &d.candidates[c]
			if d.mapAlias(cand, fieldEntry.Reference).ok {
				continue
			}

			// Both sides are sorted by key, so each candidate's iterator only moves forward.
			contextIter := &cand.contextIter
			for !contextIter.IsDone() && contextIter.GetKey() < fieldKey {
				contextIter.Next()
			}
			if !contextIter.IsDone() && contextIter.GetKey() == fieldKey {
				d.reqs = append(d.reqs, request{
					contextIdx: cand.contextIdx,
					primaryIdx: contextIter.GetIndex(),
					size:       fieldEntry.Size + 1,
				})
			}
		}
	}
	d.requestOffsets = append(d.requestOffsets, len(d.reqs))
	return offset
}

// assembleMapPatch records the candidate's patch for the target map idx, and keeps it if it is the cheapest for its
// request so far.
func (d *differ) assembleMapPatch(idx int, cand *candidate, offset int) {
	primaryIdx := cand.contextIdx
	patchStart := len(d.tape)
	size := 2

	// Start from a copy when fewer deletes than aliases are needed. This is only an estimate:
	// same-key aliases are cheaper than renamed aliases, but both count equally here.
	// removedFields is walked twice on purpose: the count decides between enterCopy and enterBlank, which must
	// be on the tape before the deletes are emitted.
	isCopy := d.removedFields(primaryIdx, idx, false) < cand.aliasCount
	if isCopy {
		d.enterCopy(primaryIdx)
		size += 2 * d.removedFields(primaryIdx, idx, true)
	} else {
		d.enterBlank(primaryIdx)
	}

	for it := d.right.Iter(idx); !it.IsDone(); it.Next() {
		fieldEntry := it.GetEntry()
		fieldKey := fieldEntry.Reference.Key

		switch alias := d.mapAlias(cand, fieldEntry.Reference); {
		case alias.ok && alias.sameKey:
			// A copied object already has the field.
			if !isCopy {
				d.emit(opRec{code: codeObjectCopyField, a: alias.fieldIdx})
				size += 2
			}
		case alias.ok:
			d.emit(opRec{code: codePushFieldCopy, a: alias.fieldIdx})
			d.emit(opRec{code: codeReturnIntoObjectPop, key: fieldKey})
			size += 2 + 1 + len(fieldKey)
		default:
			// Not an alias: use a reconstructed child patch, or insert the value directly.
			fieldReq := d.childPatch(d.requestsForField(offset, fieldEntry.Reference.Index), cand.contextIdx)
			if fieldReq == nil {
				d.emit(opRec{code: codeObjectSetFieldValue, val: fieldEntry.Value, key: fieldKey})
				size += 1 + len(fieldKey) + fieldEntry.Size
				break
			}

			d.emit(opRec{code: codeInclude, a: fieldReq.patch.start, b: fieldReq.patch.end})
			size += fieldReq.size
			if d.left.Entries[fieldReq.patchSourceIdx].Reference.Key == fieldKey {
				d.emit(opRec{code: codeReturnIntoObjectSameKeyPop})
				size += 1
			} else {
				d.emit(opRec{code: codeReturnIntoObjectPop, key: fieldKey})
				size += 1 + len(fieldKey)
			}
		}
	}

	d.updateRequest(cand.requestIdx, span{patchStart, len(d.tape)}, size, primaryIdx)
}

type sliceAlias struct {
	ok             bool
	elementIdx     int
	prevIsAdjacent bool
	nextIsAdjacent bool
}

func (d *differ) addSliceCandidate(contextIdx int, requestIdx int, elementCount int) {
	d.addCandidate(contextIdx, requestIdx, len(d.sliceAliases))
	for i := 0; i < elementCount; i++ {
		d.sliceAliases = append(d.sliceAliases, sliceAlias{})
	}
}

func (d *differ) sliceAlias(cand *candidate, pos int) *sliceAlias {
	return &d.sliceAliases[cand.aliasOffset+pos]
}

func (d *differ) insertSliceAlias(cand *candidate, target mendoza.Reference, source mendoza.Reference) {
	// We assume here that you'll only invoke this method in the same order
	// as you want to build the array.

	current := d.sliceAlias(cand, target.Index)

	if current.ok && current.prevIsAdjacent {
		// Once we've found something which is adjacent. Don't look any further.
		return
	}

	var prev *sliceAlias
	if target.Index > 0 {
		prev = d.sliceAlias(cand, target.Index-1)
	}

	if prev != nil && prev.ok {
		if prev.elementIdx+1 == source.Index {
			// This one is perfectly adjacent. Use it!
			*current = sliceAlias{
				ok:             true,
				elementIdx:     source.Index,
				prevIsAdjacent: true,
			}
			prev.nextIsAdjacent = true
			return
		}

		// We want to prefer values that are _after_ the previous index. However, we can only return if we've
		// already found a value. Otherwise we must use this new value.
		if source.Index <= prev.elementIdx && current.ok {
			return
		}
	}

	if current.ok && current.elementIdx < source.Index {
		// Prefer smaller over larger
		return
	}

	*current = sliceAlias{
		ok:             true,
		elementIdx:     source.Index,
		prevIsAdjacent: false,
	}
}

func (d *differ) reconstructSlice(idx int, reqs span) {
	// Everything appended to the scratch arenas is removed again before returning.
	mark := d.mark()

	candidates := d.findSliceCandidates(idx, reqs)
	d.findAliases(idx, candidates)
	offset := d.buildSliceRequests(idx, candidates)
	d.reconstructChildren(idx, offset)
	for c := candidates.start; c < candidates.end; c++ {
		d.assembleSlicePatch(idx, &d.candidates[c], offset)
	}

	d.restore(mark)
}

// findSliceCandidates adds a candidate for each request whose primary value is an array. Unlike maps, arrays don't
// look for other candidates.
func (d *differ) findSliceCandidates(idx int, reqs span) span {
	elementCount := len(d.right.Entries[idx].Value.([]interface{}))
	start := len(d.candidates)
	for i := reqs.start; i < reqs.end; i++ {
		if primaryIdx := d.reqs[i].primaryIdx; d.left.Entries[primaryIdx].IsNonEmptySlice() {
			d.addSliceCandidate(primaryIdx, i, elementCount)
		}
	}
	return span{start, len(d.candidates)}
}

// buildSliceRequests requests a patch for each target element from each candidate's element at the same position,
// unless the candidate already has an alias for it. The requests are grouped by element; it returns their offset.
func (d *differ) buildSliceRequests(idx int, candidates span) int {
	offset := len(d.requestOffsets)
	for it := d.right.Iter(idx); !it.IsDone(); it.Next() {
		elementEntry := it.GetEntry()
		d.requestOffsets = append(d.requestOffsets, len(d.reqs))

		for c := candidates.start; c < candidates.end; c++ {
			cand := &d.candidates[c]
			contextIter := &cand.contextIter
			if contextIter.IsDone() {
				continue
			}

			if !d.sliceAlias(cand, elementEntry.Reference.Index).ok {
				d.reqs = append(d.reqs, request{
					contextIdx: cand.contextIdx,
					primaryIdx: contextIter.GetIndex(),
					size:       elementEntry.Size + 1,
				})
			}
			contextIter.Next()
		}
	}
	d.requestOffsets = append(d.requestOffsets, len(d.reqs))
	return offset
}

// assembleSlicePatch records the candidate's patch for the target array idx, and keeps it if it is the cheapest for
// its request so far.
func (d *differ) assembleSlicePatch(idx int, cand *candidate, offset int) {
	contextIdx := cand.contextIdx
	patchStart := len(d.tape)
	d.enterBlank(contextIdx)
	size := 2

	startSlice := -1
	for it := d.right.Iter(idx); !it.IsDone(); it.Next() {
		elementEntry := it.GetEntry()
		pos := elementEntry.Reference.Index

		if alias := d.sliceAlias(cand, pos); alias.ok {
			if startSlice == -1 {
				startSlice = alias.elementIdx
			}
			// Emit the slice when the run of adjacent aliases ends.
			if !alias.nextIsAdjacent {
				d.emit(opRec{code: codeArrayAppendSlice, a: startSlice, b: alias.elementIdx + 1})
				size += 3
				startSlice = -1
			}
			continue
		}

		// Not an alias: use a reconstructed child patch, or insert the value directly.
		if elementReq := d.childPatch(d.requestsForField(offset, pos), contextIdx); elementReq != nil {
			d.emit(opRec{code: codeInclude, a: elementReq.patch.start, b: elementReq.patch.end})
			d.emit(opRec{code: codeReturnIntoArrayPop})
			size += elementReq.size + 1
		} else {
			d.emit(opRec{code: codeArrayAppendValue, val: elementEntry.Value})
			size += 1 + elementEntry.Size
		}
	}

	d.updateRequest(cand.requestIdx, span{patchStart, len(d.tape)}, size, contextIdx)
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

func (d *differ) reconstructString(rightString string, reqs span) {
	for reqIdx := reqs.start; reqIdx < reqs.end; reqIdx++ {
		primaryIdx := d.reqs[reqIdx].primaryIdx

		leftString, ok := d.left.Entries[primaryIdx].Value.(string)
		if !ok {
			continue
		}

		if leftString == rightString {
			panic("unnecessary reconstruction of string")
		}

		patchStart := len(d.tape)
		d.enterBlank(primaryIdx)
		size := 2

		prefix := commonPrefix(leftString, rightString)
		if prefix > 0 {
			d.emit(opRec{code: codeStringAppendSlice, a: 0, b: prefix})
			size += 3
		}

		suffix := commonSuffix(leftString, rightString, prefix)

		mid := rightString[prefix : len(rightString)-suffix]
		if len(mid) > 0 {
			d.emit(opRec{code: codeStringAppendString, key: mid})
			size += 1 + len(mid)
		}

		if suffix > 0 {
			d.emit(opRec{code: codeStringAppendSlice, a: len(leftString) - suffix, b: len(leftString)})
			size += 3
		}

		d.updateRequest(reqIdx, span{patchStart, len(d.tape)}, size, primaryIdx)
	}
}
