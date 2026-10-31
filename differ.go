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
func (options *Options) CreatePatch(left, right interface{}) (Patch, error) {
	return options.NewDiffer().CreatePatch(left, right)
}

// Creates two patches: The first can be applied to the left document to produce the right document,
// the second can be applied to the right document to produce the left document.
//
// Use a Differ instead to reuse memory between calls.
func (options *Options) CreateDoublePatch(left, right interface{}) (Patch, Patch, error) {
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

	if err := d.fill(left, right); err != nil {
		return err
	}
	if d.left.Entries[0].Hash == d.right.Entries[0].Hash {
		return nil
	}

	d.hashIndex.Build(&d.left)
	d.patch = d.differ.build(&d.left, &d.right, &d.hashIndex)
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

	if err := d.fill(left, right); err != nil {
		return err
	}
	if d.left.Entries[0].Hash == d.right.Entries[0].Hash {
		return nil
	}

	d.hashIndex.Build(&d.left)
	d.patch = d.differ.build(&d.left, &d.right, &d.hashIndex)

	d.hashIndex.Build(&d.right)
	d.reversePatch = d.differ.build(&d.right, &d.left, &d.hashIndex)
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

func (d *Differ) fill(left, right interface{}) error {
	if err := d.left.Fill(left, d.options.convertFunc); err != nil {
		return err
	}
	return d.right.Fill(right, d.options.convertFunc)
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

Patches are recorded as ops on a tape. Unlike scratch data, tape records survive recursive calls so a parent
can include a child's patch by span (codeInclude) instead of copying it. Patch sizes are heuristic costs,
not operation counts or exact serialized byte lengths.

*/

// span refers to the range [start, end) in one of the differ's slices.
type span struct {
	start, end int
}

// opRec is an op recorded on the tape. Which of a, b, key and val are used depends on the code.
type opRec struct {
	code uint8
	a, b int
	key  string
	val  interface{}
}

// codeInclude is only used on the tape: it stands for all the ops in tape[a:b].
// Included spans are recorded before their parent patch, so these references cannot form cycles.
const codeInclude uint8 = 255

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

func (d *differ) reset() {
	// Drop references to the previous documents.
	for i := range d.tape {
		d.tape[i] = opRec{}
	}

	d.tape = d.tape[:0]
	d.reqs = d.reqs[:0]
	d.requestOffsets = d.requestOffsets[:0]
	d.candidates = d.candidates[:0]
	d.mapAliases = d.mapAliases[:0]
	d.sliceAliases = d.sliceAliases[:0]
}

func (d *differ) emit(op opRec) {
	d.tape = append(d.tape, op)
}

func (d *differ) valueSpan(value interface{}) span {
	start := len(d.tape)
	d.emit(opRec{code: codeValue, val: value})
	return span{start, len(d.tape)}
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

	d.reconstruct(0, span{0, 1})

	req := d.reqs[0]
	d.reqs = d.reqs[:0]

	if !req.hasPatch {
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

func (req *request) update(patch span, size int, patchSourceIdx int) {
	if size < req.size {
		req.patch = patch
		req.hasPatch = true
		req.size = size
		req.patchSourceIdx = patchSourceIdx
	}
}

// requestsForField returns the request range for a field or element. offset is the start of this container's
// entries in requestOffsets. Each container records one offset per field/element, plus a final end offset.
func (d *differ) requestsForField(offset int, field int) span {
	return span{d.requestOffsets[offset+field], d.requestOffsets[offset+field+1]}
}

// requestForContext returns the first matching request, even if it has no patch. Later requests with the same
// context must not override that choice. The returned pointer is only valid until the next append to d.reqs.
func (d *differ) requestForContext(reqs span, contextIdx int) *request {
	for i := reqs.start; i < reqs.end; i++ {
		if d.reqs[i].contextIdx == contextIdx {
			return &d.reqs[i]
		}
	}
	return nil
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
	// contextIter walks the candidate's fields/elements while the requests are built.
	contextIter mendoza.Iter
}

type mapAlias struct {
	ok       bool
	fieldIdx int
	sameKey  bool
}

func (d *differ) addMapCandidate(contextIdx int, requestIdx int, fieldCount int) {
	d.candidates = append(d.candidates, candidate{
		contextIdx:  contextIdx,
		requestIdx:  requestIdx,
		aliasOffset: len(d.mapAliases),
		contextIter: d.left.Iter(contextIdx),
	})
	for i := 0; i < fieldCount; i++ {
		d.mapAliases = append(d.mapAliases, mapAlias{})
	}
}

func (d *differ) mapAlias(cand *candidate, target mendoza.Reference) *mapAlias {
	return &d.mapAliases[cand.aliasOffset+target.Index]
}

func (d *differ) insertMapAlias(cand *candidate, target mendoza.Reference, source mendoza.Reference) {
	current := d.mapAlias(cand, target)

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

	entry := &d.right.Entries[idx]
	fieldCount := len(entry.Value.(map[string]interface{}))

	// Everything appended to these is removed again before returning.
	reqsMark := len(d.reqs)
	candidatesMark := len(d.candidates)
	aliasesMark := len(d.mapAliases)
	offsetsMark := len(d.requestOffsets)

	// Find candidates: start with each request's primary value.
	for i := reqs.start; i < reqs.end; i++ {
		req := &d.reqs[i]
		if !d.left.Entries[req.primaryIdx].IsNonEmptyMap() {
			continue
		}

		d.addMapCandidate(req.primaryIdx, i, fieldCount)
	}

	// Also try sibling objects that match after removing one field's value hash.
	for it := d.right.Iter(idx); !it.IsDone(); it.Next() {
		fieldEntry := it.GetEntry()

		xorHash := entry.XorHash
		xorHash.Xor(fieldEntry.Hash)

		for others := d.hashIndex.LookupXor(xorHash); ; {
			otherIdx, ok := others.Next()
			if !ok {
				break
			}
			otherEntry := &d.left.Entries[otherIdx]

			for i := reqs.start; i < reqs.end; i++ {
				req := &d.reqs[i]
				if otherEntry.Parent == req.contextIdx && otherIdx != req.primaryIdx {
					d.addMapCandidate(otherIdx, i, fieldCount)
				}
			}
		}
	}

	candidates := span{candidatesMark, len(d.candidates)}

	// Find exact aliases within each candidate.
	for it := d.right.Iter(idx); !it.IsDone(); it.Next() {
		fieldEntry := it.GetEntry()

		for others := d.hashIndex.Lookup(fieldEntry.Hash); ; {
			otherIdx, ok := others.Next()
			if !ok {
				break
			}
			otherEntry := &d.left.Entries[otherIdx]

			for c := candidates.start; c < candidates.end; c++ {
				cand := &d.candidates[c]
				if cand.contextIdx == otherEntry.Parent {
					d.insertMapAlias(cand, fieldEntry.Reference, otherEntry.Reference)
				}
			}
		}
	}

	// Build child requests, grouped by target field.
	fieldRequestsOffset := len(d.requestOffsets)
	for it := d.right.Iter(idx); !it.IsDone(); it.Next() {
		fieldEntry := it.GetEntry()
		fieldKey := fieldEntry.Reference.Key
		d.requestOffsets = append(d.requestOffsets, len(d.reqs))

		for c := candidates.start; c < candidates.end; c++ {
			cand := &d.candidates[c]
			contextIter := &cand.contextIter

			if !d.mapAlias(cand, fieldEntry.Reference).ok {
				for !contextIter.IsDone() {
					key := contextIter.GetKey()

					if key > fieldKey {
						break
					}

					if key == fieldKey {
						d.reqs = append(d.reqs, request{
							contextIdx: cand.contextIdx,
							primaryIdx: contextIter.GetIndex(),
							size:       fieldEntry.Size + 1,
						})
						break
					}

					contextIter.Next()
				}
			}
		}
	}
	d.requestOffsets = append(d.requestOffsets, len(d.reqs))

	// Reconstruct each target field for its requested contexts.
	for it := d.right.Iter(idx); !it.IsDone(); it.Next() {
		d.reconstruct(it.GetIndex(), d.requestsForField(fieldRequestsOffset, it.GetEntry().Reference.Index))
	}

	// Assemble each candidate's patch and keep the cheapest for its request.
	for c := candidates.start; c < candidates.end; c++ {
		cand := &d.candidates[c]
		primaryIdx := cand.contextIdx

		size := 0
		patchStart := len(d.tape)

		removeCount := d.removedFields(primaryIdx, idx, false)
		aliasCount := 0
		for i := 0; i < fieldCount; i++ {
			if d.mapAliases[cand.aliasOffset+i].ok {
				aliasCount++
			}
		}

		// Start from a copy when fewer deletes than aliases are needed. This is only an estimate:
		// same-key aliases are cheaper than renamed aliases, but both count equally here.
		isCopy := removeCount < aliasCount

		if isCopy {
			d.enterCopy(primaryIdx)
		} else {
			d.enterBlank(primaryIdx)
		}

		size += 2

		if isCopy {
			// Delete fields we don't need
			size += 2 * d.removedFields(primaryIdx, idx, true)
		}

		for it := d.right.Iter(idx); !it.IsDone(); it.Next() {
			fieldEntry := it.GetEntry()
			fieldKey := fieldEntry.Reference.Key

			if alias := d.mapAlias(cand, fieldEntry.Reference); alias.ok {
				if alias.sameKey {
					if !isCopy {
						// We only need this if we're starting with a blank object
						d.emit(opRec{code: codeObjectCopyField, a: alias.fieldIdx})
						size += 2
					}
				} else {
					d.emit(opRec{code: codePushFieldCopy, a: alias.fieldIdx})
					d.emit(opRec{code: codeReturnIntoObjectPop, key: fieldKey})
					size += 2 + 1 + len(fieldKey)
				}
			} else {
				// Not an alias: use a reconstructed child patch, or insert the value directly.
				fieldReqs := d.requestsForField(fieldRequestsOffset, fieldEntry.Reference.Index)
				fieldReq := d.requestForContext(fieldReqs, cand.contextIdx)
				if fieldReq != nil && fieldReq.hasPatch {
					d.emit(opRec{code: codeInclude, a: fieldReq.patch.start, b: fieldReq.patch.end})
					size += fieldReq.size

					if d.left.Entries[fieldReq.patchSourceIdx].Reference.Key == fieldKey {
						d.emit(opRec{code: codeReturnIntoObjectSameKeyPop})
						size += 1
					} else {
						d.emit(opRec{code: codeReturnIntoObjectPop, key: fieldKey})
						size += 1 + len(fieldKey)
					}
				} else {
					d.emit(opRec{code: codeObjectSetFieldValue, val: fieldEntry.Value, key: fieldKey})
					size += 1 + len(fieldKey) + fieldEntry.Size
				}
			}
		}

		patch := span{patchStart, len(d.tape)}
		d.reqs[cand.requestIdx].update(patch, size, primaryIdx)
	}

	// Restore scratch slices; the parent still needs the patches on the tape.
	d.reqs = d.reqs[:reqsMark]
	d.candidates = d.candidates[:candidatesMark]
	d.mapAliases = d.mapAliases[:aliasesMark]
	d.requestOffsets = d.requestOffsets[:offsetsMark]
}

type sliceAlias struct {
	ok             bool
	elementIdx     int
	prevIsAdjacent bool
	nextIsAdjacent bool
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

	if target.Index > 0 {
		if prevSource := d.sliceAlias(cand, target.Index-1); prevSource.ok {
			if prevSource.elementIdx+1 == source.Index {
				// This one is perfectly adjacent. Use it!
				*current = sliceAlias{
					ok:             true,
					elementIdx:     source.Index,
					prevIsAdjacent: true,
				}
				prevSource.nextIsAdjacent = true
				return
			}

			if source.Index <= prevSource.elementIdx {
				// We want to prefer values that are _after_ the previous index.

				if current.ok {
					// However, we can only return if we've already found a value.
					// Otherwise we must use this new value.
					return
				}
			}
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
	entry := &d.right.Entries[idx]
	elementCount := len(entry.Value.([]interface{}))

	// Everything appended to these is removed again before returning.
	reqsMark := len(d.reqs)
	candidatesMark := len(d.candidates)
	aliasesMark := len(d.sliceAliases)
	offsetsMark := len(d.requestOffsets)

	// Find candidates: arrays only use each request's primary value.
	for i := reqs.start; i < reqs.end; i++ {
		req := &d.reqs[i]
		if !d.left.Entries[req.primaryIdx].IsNonEmptySlice() {
			continue
		}

		d.candidates = append(d.candidates, candidate{
			contextIdx:  req.primaryIdx,
			requestIdx:  i,
			aliasOffset: len(d.sliceAliases),
			contextIter: d.left.Iter(req.primaryIdx),
		})
		for j := 0; j < elementCount; j++ {
			d.sliceAliases = append(d.sliceAliases, sliceAlias{})
		}
	}

	candidates := span{candidatesMark, len(d.candidates)}

	// Find exact aliases within each candidate, preserving target order for adjacent slices.
	for it := d.right.Iter(idx); !it.IsDone(); it.Next() {
		elementEntry := it.GetEntry()

		for others := d.hashIndex.Lookup(elementEntry.Hash); ; {
			otherIdx, ok := others.Next()
			if !ok {
				break
			}
			otherEntry := &d.left.Entries[otherIdx]

			for c := candidates.start; c < candidates.end; c++ {
				cand := &d.candidates[c]
				if cand.contextIdx == otherEntry.Parent {
					d.insertSliceAlias(cand, elementEntry.Reference, otherEntry.Reference)
				}
			}
		}
	}

	// Build child requests, grouped by target element.
	elementRequestsOffset := len(d.requestOffsets)
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

	// Reconstruct each target element for its requested contexts.
	for it := d.right.Iter(idx); !it.IsDone(); it.Next() {
		d.reconstruct(it.GetIndex(), d.requestsForField(elementRequestsOffset, it.GetEntry().Reference.Index))
	}

	// Assemble each candidate's patch and keep the cheapest for its request.
	for c := candidates.start; c < candidates.end; c++ {
		cand := &d.candidates[c]
		contextIdx := cand.contextIdx
		size := 0
		patchStart := len(d.tape)

		d.enterBlank(contextIdx)
		size += 2

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
			} else {
				// Not an alias: use a reconstructed child patch, or insert the value directly.
				elementReqs := d.requestsForField(elementRequestsOffset, pos)
				elementReq := d.requestForContext(elementReqs, contextIdx)
				if elementReq != nil && elementReq.hasPatch {
					d.emit(opRec{code: codeInclude, a: elementReq.patch.start, b: elementReq.patch.end})
					size += elementReq.size
					d.emit(opRec{code: codeReturnIntoArrayPop})
					size += 1
				} else {
					d.emit(opRec{code: codeArrayAppendValue, val: elementEntry.Value})
					size += 1 + elementEntry.Size
				}
			}
		}

		patch := span{patchStart, len(d.tape)}
		d.reqs[cand.requestIdx].update(patch, size, contextIdx)
	}

	// Restore scratch slices; the parent still needs the patches on the tape.
	d.reqs = d.reqs[:reqsMark]
	d.candidates = d.candidates[:candidatesMark]
	d.sliceAliases = d.sliceAliases[:aliasesMark]
	d.requestOffsets = d.requestOffsets[:offsetsMark]
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
		leftEntry := &d.left.Entries[primaryIdx]

		leftString, ok := leftEntry.Value.(string)
		if !ok {
			continue
		}

		if leftString == rightString {
			panic("unnecessary reconstruction of string")
		}

		size := 0
		patchStart := len(d.tape)

		d.enterBlank(primaryIdx)
		size += 2

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

		patch := span{patchStart, len(d.tape)}
		d.reqs[reqIdx].update(patch, size, primaryIdx)
	}
}

// Output

// writeSpan writes the ops in tape[sp.start:sp.end] in the same format as Patch.WriteTo.
func (d *differ) writeSpan(w Writer, sp span) error {
	for i := sp.start; i < sp.end; i++ {
		op := &d.tape[i]

		if op.code == codeInclude {
			if err := d.writeSpan(w, span{op.a, op.b}); err != nil {
				return err
			}
			continue
		}

		if err := w.WriteUint8(op.code); err != nil {
			return err
		}

		var err error
		switch op.code {
		case codeValue, codeArrayAppendValue:
			err = w.WriteValue(op.val)
		case codePushFieldBlank, codePushFieldCopy, codePushElementBlank, codePushElementCopy,
			codeObjectCopyField, codeObjectDeleteField:
			err = w.WriteUint(op.a)
		case codeReturnIntoObjectPop, codeStringAppendString:
			err = w.WriteString(op.key)
		case codeObjectSetFieldValue:
			if err = w.WriteValue(op.val); err == nil {
				err = w.WriteString(op.key)
			}
		case codeArrayAppendSlice, codeStringAppendSlice:
			if err = w.WriteUint(op.a); err == nil {
				err = w.WriteUint(op.b)
			}
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// patchFor converts the ops in tape[sp.start:sp.end] into a Patch.
func (d *differ) patchFor(sp span) Patch {
	return d.appendOps(make(Patch, 0, d.countOps(sp)), sp)
}

func (d *differ) countOps(sp span) int {
	count := 0
	for i := sp.start; i < sp.end; i++ {
		if op := &d.tape[i]; op.code == codeInclude {
			count += d.countOps(span{op.a, op.b})
		} else {
			count++
		}
	}
	return count
}

func (d *differ) appendOps(patch Patch, sp span) Patch {
	for i := sp.start; i < sp.end; i++ {
		if op := &d.tape[i]; op.code == codeInclude {
			patch = d.appendOps(patch, span{op.a, op.b})
		} else {
			patch = append(patch, op.toOp())
		}
	}
	return patch
}

func (op *opRec) toOp() Op {
	switch op.code {
	case codeValue:
		return &OpValue{Value: op.val}
	case codeBlank:
		return &OpBlank{}
	case codePushFieldCopy:
		return &OpPushFieldCopy{OpPushField: OpPushField{Index: op.a}}
	case codePushFieldBlank:
		return &OpPushFieldBlank{OpPushField: OpPushField{Index: op.a}}
	case codePushElementCopy:
		return &OpPushElementCopy{OpPushElement: OpPushElement{Index: op.a}}
	case codePushElementBlank:
		return &OpPushElementBlank{OpPushElement: OpPushElement{Index: op.a}}
	case codeReturnIntoObjectPop:
		return &OpReturnIntoObjectPop{OpReturnIntoObject: OpReturnIntoObject{Key: op.key}}
	case codeReturnIntoObjectSameKeyPop:
		return &OpReturnIntoObjectSameKeyPop{}
	case codeReturnIntoArrayPop:
		return &OpReturnIntoArrayPop{}
	case codeObjectSetFieldValue:
		return &OpObjectSetFieldValue{OpValue: OpValue{Value: op.val}, OpReturnIntoObject: OpReturnIntoObject{Key: op.key}}
	case codeObjectCopyField:
		return &OpObjectCopyField{OpPushField: OpPushField{Index: op.a}}
	case codeObjectDeleteField:
		return &OpObjectDeleteField{Index: op.a}
	case codeArrayAppendValue:
		return &OpArrayAppendValue{Value: op.val}
	case codeArrayAppendSlice:
		return &OpArrayAppendSlice{Left: op.a, Right: op.b}
	case codeStringAppendString:
		return &OpStringAppendString{String: op.key}
	case codeStringAppendSlice:
		return &OpStringAppendSlice{Left: op.a, Right: op.b}
	}
	panic("mendoza: unexpected op on tape")
}
