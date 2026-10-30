package mendoza

import (
	"slices"
	"sync"
	"unicode/utf8"

	"github.com/sanity-io/mendoza/internal/mendoza"
)

// Differ creates patches and owns all of the working memory needed to do so.
// Reusing a Differ across calls makes diffing allocation-free once its
// buffers have grown to fit the largest documents seen.
//
// A Differ is not safe for concurrent use.
type Differ struct {
	options           Options
	left, right       mendoza.HashList
	leftIdx, rightIdx mendoza.HashIndex
	st                differ
	results           [2]span
	nresults          int
	stack             []span
}

// NewDiffer returns a reusable Differ using the given options.
func (options Options) NewDiffer() *Differ {
	return &Differ{options: options}
}

// NewDiffer returns a reusable Differ using the default options.
func NewDiffer() *Differ { return DefaultOptions.NewDiffer() }

var differPool = sync.Pool{New: func() interface{} { return new(Differ) }}

func getDiffer(options *Options) *Differ {
	d := differPool.Get().(*Differ)
	d.options = *options
	return d
}

func putDiffer(d *Differ) {
	d.reset()
	d.options = Options{}
	differPool.Put(d)
}

func (d *Differ) reset() {
	d.left.Reset(nil)
	d.right.Reset(nil)
	d.st.reset()
	d.nresults = 0
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
func (options *Options) CreatePatch(left, right interface{}) (Patch, error) {
	if left == nil {
		if right == nil {
			return Patch{}, nil
		}
		return Patch{&OpValue{right}}, nil
	}
	d := getDiffer(options)
	defer putDiffer(d)
	return d.CreatePatch(left, right)
}

// Creates two patches: The first can be applied to the left document to produce the right document,
// the second can be applied to the right document to produce the left document.
func (options *Options) CreateDoublePatch(left, right interface{}) (Patch, Patch, error) {
	if left == nil && right == nil {
		return Patch{}, Patch{}, nil
	}
	if left == nil {
		return Patch{&OpValue{right}}, Patch{&OpValue{nil}}, nil
	}
	if right == nil {
		return Patch{&OpValue{nil}}, Patch{&OpValue{left}}, nil
	}
	d := getDiffer(options)
	defer putDiffer(d)
	return d.CreateDoublePatch(left, right)
}

// CreatePatch is like Options.CreatePatch but reuses the Differ's memory.
// Only the returned Patch is allocated.
func (d *Differ) CreatePatch(left, right interface{}) (Patch, error) {
	if left == nil {
		if right == nil {
			return Patch{}, nil
		}
		return Patch{&OpValue{right}}, nil
	}
	if err := d.Diff(left, right); err != nil {
		return nil, err
	}
	return d.materialize(d.results[0]), nil
}

// CreateDoublePatch is like Options.CreateDoublePatch but reuses the Differ's memory.
func (d *Differ) CreateDoublePatch(left, right interface{}) (Patch, Patch, error) {
	if left == nil || right == nil {
		return d.options.CreateDoublePatch(left, right)
	}
	if err := d.DiffDouble(left, right); err != nil {
		return nil, nil, err
	}
	return d.materialize(d.results[0]), d.materialize(d.results[1]), nil
}

// Diff computes the patch from left to right without allocating it. The result
// can be read with WriteTo and stays valid until the next call on the Differ.
func (d *Differ) Diff(left, right interface{}) error {
	d.reset()
	if left == nil {
		d.results[0] = d.st.valueSpan(right)
		d.nresults = 1
		return nil
	}
	if err := d.hash(left, right); err != nil {
		return err
	}
	d.leftIdx.Build(&d.left)
	d.st.use(&d.left, &d.right, &d.leftIdx)
	d.results[0] = d.st.build()
	d.nresults = 1
	return nil
}

// DiffDouble computes both the left→right and right→left patches. Use
// WriteTo and WriteReverseTo to read them.
func (d *Differ) DiffDouble(left, right interface{}) error {
	d.reset()
	if left == nil || right == nil {
		d.results[0] = d.st.valueSpan(right)
		d.results[1] = d.st.valueSpan(left)
		d.nresults = 2
		return nil
	}
	if err := d.hash(left, right); err != nil {
		return err
	}
	d.leftIdx.Build(&d.left)
	d.st.use(&d.left, &d.right, &d.leftIdx)
	d.results[0] = d.st.build()
	d.rightIdx.Build(&d.right)
	d.st.use(&d.right, &d.left, &d.rightIdx)
	d.results[1] = d.st.build()
	d.nresults = 2
	return nil
}

func (d *Differ) hash(left, right interface{}) error {
	if err := d.left.Fill(left, d.options.convertFunc); err != nil {
		return err
	}
	return d.right.Fill(right, d.options.convertFunc)
}

// WriteTo writes the patch computed by the last Diff to w.
func (d *Differ) WriteTo(w Writer) error {
	if d.nresults < 1 {
		return nil
	}
	return d.writeSpan(w, d.results[0])
}

// WriteReverseTo writes the right→left patch computed by the last DiffDouble to w.
func (d *Differ) WriteReverseTo(w Writer) error {
	if d.nresults < 2 {
		return nil
	}
	return d.writeSpan(w, d.results[1])
}

/*

The main function in the differ is `reconstruct` which takes two parameters.
The first is the target value: a reference to a value in the right-hand side.
The second parameter is a slice of _requests_. A request has an _initial
context_ which is a reference to a value in the left-hand side. The goal of
`reconstruct` is then, for each request, build a patch which converts the
_initial context_ into the target value.  Every request also keeps track of the
_best size_.

See the git history for a longer description of the algorithm. This version
is allocation-free: all working state lives in arenas owned by the differ,
addressed by int32 offsets, and truncated back to a mark when each level of the
recursion returns. Patches are recorded on an op tape; a parent includes a
child's patch by reference (codeInclude) instead of copying it.

*/

// codeInclude is a tape-only pseudo op: "emit the ops in tape[a:b]".
const codeInclude uint8 = 255

type opRec struct {
	code uint8
	a, b int32
	key  string // key or string payload, borrowed from the input
	val  interface{}
}

type span struct{ start, end int32 }

type request struct {
	contextIdx int32
	primaryIdx int32
	size       int
	patch      span
	hasPatch   bool
	outputKey  string
}

type pendingReq struct {
	field int32
	req   request
}

type mapAlias struct {
	fieldIdx int32
	sameKey  bool
	valid    bool
}

type mapCandidate struct {
	contextIdx int32
	requestIdx int32
	aliasOff   int32
	kidsOff    int32
	kidsEnd    int32
}

type sliceAlias struct {
	elementIdx     int32
	prevIsAdjacent bool
	nextIsAdjacent bool
	valid          bool
}

type sliceCandidate struct {
	contextIdx int32
	requestIdx int32
	aliasOff   int32
	kidsOff    int32
	kidsEnd    int32
}

// kid is a child of a candidate, kept sorted by (hash, idx) so the children
// matching a given hash can be found by binary search.
type kid struct {
	h1, h2 uint64
	idx    int32
}

func kidLess(a, b kid) int {
	switch {
	case a.h1 != b.h1:
		if a.h1 < b.h1 {
			return -1
		}
		return 1
	case a.h2 != b.h2:
		if a.h2 < b.h2 {
			return -1
		}
		return 1
	case a.idx < b.idx:
		return -1
	case a.idx > b.idx:
		return 1
	}
	return 0
}

type differ struct {
	left, right *mendoza.HashList
	hashIndex   *mendoza.HashIndex
	kids        []kid

	tape       []opRec
	reqs       []request
	pend       []pendingReq
	ranges     []int32
	mapCands   []mapCandidate
	mapAlias   []mapAlias
	sliceCands []sliceCandidate
	sliceAlias []sliceAlias
}

func (d *differ) reset() {
	clear(d.tape) // drop references to user values
	d.tape = d.tape[:0]
	clear(d.reqs)
	d.reqs = d.reqs[:0]
	d.pend = d.pend[:0]
	d.ranges = d.ranges[:0]
	d.mapCands = d.mapCands[:0]
	d.mapAlias = d.mapAlias[:0]
	d.sliceCands = d.sliceCands[:0]
	d.sliceAlias = d.sliceAlias[:0]
	d.kids = d.kids[:0]
}

// indexKids appends the children of contextIdx to the kids arena, sorted by
// hash, and returns their range.
func (d *differ) indexKids(contextIdx int32) (int32, int32) {
	off := int32(len(d.kids))
	for it := d.left.Iter(int(contextIdx)); !it.IsDone(); it.Next() {
		a, b := it.GetEntry().Hash.Words()
		d.kids = append(d.kids, kid{a, b, int32(it.GetIndex())})
	}
	slices.SortFunc(d.kids[off:], kidLess)
	return off, int32(len(d.kids))
}

// matchKids returns the sub-range of kids[off:end] whose hash equals h,
// in ascending entry order.
func (d *differ) matchKids(off, end int32, h mendoza.Hash) (int32, int32) {
	a, b := h.Words()
	ks := d.kids[off:end]
	lo, _ := slices.BinarySearchFunc(ks, kid{a, b, -1 << 31}, kidLess)
	hi := lo
	for hi < len(ks) && ks[hi].h1 == a && ks[hi].h2 == b {
		hi++
	}
	return off + int32(lo), off + int32(hi)
}

func (d *differ) use(left, right *mendoza.HashList, idx *mendoza.HashIndex) {
	d.left, d.right, d.hashIndex = left, right, idx
}

// grow extends s by n zeroed elements, reusing capacity where possible.
func grow[T any](s []T, n int) []T {
	l := len(s)
	if cap(s)-l < n {
		ns := make([]T, l, 2*cap(s)+n)
		copy(ns, s)
		s = ns
	}
	s = s[:l+n]
	clear(s[l:])
	return s
}

func (d *differ) emit(op opRec) { d.tape = append(d.tape, op) }

func (d *differ) valueSpan(v interface{}) span {
	start := int32(len(d.tape))
	d.emit(opRec{code: codeValue, val: v})
	return span{start, int32(len(d.tape))}
}

func (d *differ) build() span {
	root := &d.right.Entries[0]
	start := int32(len(d.tape))

	if d.left.Entries[0].Hash == root.Hash {
		// Exact same value: empty patch.
		return span{start, start}
	}

	off := int32(len(d.reqs))
	d.reqs = append(d.reqs, request{contextIdx: -1, primaryIdx: 0, size: root.Size + 1})
	d.reconstruct(0, off, 1)
	req := d.reqs[off]
	d.reqs = d.reqs[:off]

	if !req.hasPatch {
		return d.valueSpan(root.Value)
	}
	return req.patch
}

func (d *differ) update(reqIdx int32, patch span, size int, outputKey string) {
	req := &d.reqs[reqIdx]
	if size < req.size {
		req.patch = patch
		req.hasPatch = true
		req.size = size
		req.outputKey = outputKey
	}
}

func (d *differ) reconstruct(idx int, off, n int32) {
	if n == 0 {
		return
	}

	entry := &d.right.Entries[idx]

	if entry.IsNonEmptyMap() {
		d.reconstructMap(idx, off, n)
		return
	}

	if entry.IsNonEmptySlice() {
		d.reconstructSlice(idx, off, n)
		return
	}

	if rightString, ok := entry.Value.(string); ok {
		d.reconstructString(rightString, off, n)
		return
	}
}

func (d *differ) enterBlank(idx int32) {
	if idx == 0 {
		d.emit(opRec{code: codeBlank})
		return
	}
	entry := &d.left.Entries[idx]
	if d.left.Entries[entry.Parent].IsNonEmptyMap() {
		d.emit(opRec{code: codePushFieldBlank, a: int32(entry.Reference.Index)})
	} else {
		d.emit(opRec{code: codePushElementBlank, a: int32(entry.Reference.Index)})
	}
}

func (d *differ) enterCopy(idx int32) {
	if idx == 0 {
		// Root => Already on the stack.
		return
	}
	entry := &d.left.Entries[idx]
	if d.left.Entries[entry.Parent].IsNonEmptyMap() {
		d.emit(opRec{code: codePushFieldCopy, a: int32(entry.Reference.Index)})
	} else {
		d.emit(opRec{code: codePushElementCopy, a: int32(entry.Reference.Index)})
	}
}

// findRequest returns the first request for the given field whose context
// matches, or -1. (Same "first match" semantics as the original.)
func (d *differ) findRequest(start, end int32, contextIdx int32) int32 {
	for i := start; i < end; i++ {
		if d.reqs[i].contextIdx == contextIdx {
			return i
		}
	}
	return -1
}

// placeRequests moves pending requests pend[pMark:] into the request arena,
// grouped by field (stable). It returns the offset of a ranges block where
// field i's requests live at reqs[r[i]:r[i+1]] (absolute indices).
func (d *differ) placeRequests(pMark int, k int) int {
	rg := len(d.ranges)
	d.ranges = grow(d.ranges, 2*(k+1))
	starts := d.ranges[rg : rg+k+1]
	cursor := d.ranges[rg+k+1 : rg+2*(k+1)]

	for _, p := range d.pend[pMark:] {
		starts[p.field+1]++
	}
	base := int32(len(d.reqs))
	starts[0] = base
	for i := 1; i <= k; i++ {
		starts[i] += starts[i-1]
	}
	copy(cursor, starts)

	d.reqs = grow(d.reqs, len(d.pend)-pMark)
	for _, p := range d.pend[pMark:] {
		d.reqs[cursor[p.field]] = p.req
		cursor[p.field]++
	}
	d.pend = d.pend[:pMark]
	return rg
}

// Maps

func (d *differ) newMapCand(contextIdx, reqIdx int32, k int) {
	d.mapCands = append(d.mapCands, mapCandidate{
		contextIdx: contextIdx,
		requestIdx: reqIdx,
		aliasOff:   int32(len(d.mapAlias)),
	})
	d.mapAlias = grow(d.mapAlias, k)
}

func (d *differ) insertMapAlias(cand *mapCandidate, target, source *mendoza.Reference) {
	a := &d.mapAlias[cand.aliasOff+int32(target.Index)]

	if target.Key == source.Key {
		*a = mapAlias{fieldIdx: int32(source.Index), sameKey: true, valid: true}
		return
	}
	if a.valid {
		if a.sameKey {
			// Prefer sameKey
			return
		}
		if a.fieldIdx < int32(source.Index) {
			// Prefer lowest fieldIdx to ensure consistent patches
			return
		}
	}
	*a = mapAlias{fieldIdx: int32(source.Index), valid: true}
}

func (d *differ) reconstructMap(idx int, off, n int32) {
	entry := &d.right.Entries[idx]
	k := len(entry.Value.(map[string]interface{}))

	reqMark := len(d.reqs)
	candMark := len(d.mapCands)
	aliasMark := len(d.mapAlias)
	rangeMark := len(d.ranges)

	// Candidates: the primary of each request...
	for i := off; i < off+n; i++ {
		req := &d.reqs[i]
		if d.left.Entries[req.primaryIdx].IsNonEmptyMap() {
			d.newMapCand(req.primaryIdx, i, k)
		}
	}

	// ...plus siblings that differ by one field (via the xor index).
	for it := d.right.Iter(idx); !it.IsDone(); it.Next() {
		xorHash := entry.XorHash
		xorHash.Xor(it.GetEntry().Hash)

		for ci := d.hashIndex.LookupXor(xorHash); ; {
			otherIdx, ok := ci.Next()
			if !ok {
				break
			}
			otherParent := int32(d.left.Entries[otherIdx].Parent)
			for i := off; i < off+n; i++ {
				req := &d.reqs[i]
				if otherParent == req.contextIdx && int32(otherIdx) != req.primaryIdx {
					d.newMapCand(int32(otherIdx), i, k)
				}
			}
		}
	}

	// Find fields that exist verbatim in each candidate. Candidates are
	// independent, so matching each candidate's own children (sorted by hash)
	// visits the same entries in the same order as scanning every entry in the
	// document with that hash and filtering by parent - without the O(n) scan.
	kidsMark := len(d.kids)
	for c := candMark; c < len(d.mapCands); c++ {
		cand := &d.mapCands[c]
		cand.kidsOff, cand.kidsEnd = d.indexKids(cand.contextIdx)
	}
	for it := d.right.Iter(idx); !it.IsDone(); it.Next() {
		fieldEntry := it.GetEntry()
		for c := candMark; c < len(d.mapCands); c++ {
			cand := &d.mapCands[c]
			lo, hi := d.matchKids(cand.kidsOff, cand.kidsEnd, fieldEntry.Hash)
			for ; lo < hi; lo++ {
				other := &d.left.Entries[d.kids[lo].idx]
				d.insertMapAlias(cand, &fieldEntry.Reference, &other.Reference)
			}
		}
	}
	d.kids = d.kids[:kidsMark]

	// Build the requests for fields that need a real diff.
	pMark := len(d.pend)
	for c := candMark; c < len(d.mapCands); c++ {
		cand := d.mapCands[c]
		contextIter := d.left.Iter(int(cand.contextIdx))

		for it := d.right.Iter(idx); !it.IsDone(); it.Next() {
			fieldEntry := it.GetEntry()
			pos := fieldEntry.Reference.Index
			if d.mapAlias[cand.aliasOff+int32(pos)].valid {
				continue
			}
			fieldKey := fieldEntry.Reference.Key
			for !contextIter.IsDone() {
				key := contextIter.GetKey()
				if key > fieldKey {
					break
				}
				if key == fieldKey {
					d.pend = append(d.pend, pendingReq{
						field: int32(pos),
						req: request{
							contextIdx: cand.contextIdx,
							primaryIdx: int32(contextIter.GetIndex()),
							size:       fieldEntry.Size + 1,
						},
					})
					break
				}
				contextIter.Next()
			}
		}
	}
	rg := d.placeRequests(pMark, k)

	for it := d.right.Iter(idx); !it.IsDone(); it.Next() {
		pos := it.GetEntry().Reference.Index
		start, end := d.ranges[rg+pos], d.ranges[rg+pos+1]
		d.reconstruct(it.GetIndex(), start, end-start)
	}

	// Assemble a patch per candidate.
	for c := candMark; c < len(d.mapCands); c++ {
		cand := d.mapCands[c]
		primaryIdx := cand.contextIdx

		// Fields present in the candidate but not in the target.
		removeCount := 0
		rit := d.right.Iter(idx)
		for lit := d.left.Iter(int(primaryIdx)); !lit.IsDone(); lit.Next() {
			lk := lit.GetKey()
			for !rit.IsDone() && rit.GetKey() < lk {
				rit.Next()
			}
			if rit.IsDone() || rit.GetKey() != lk {
				removeCount++
			}
		}

		aliasCount := 0
		for _, a := range d.mapAlias[cand.aliasOff : cand.aliasOff+int32(k)] {
			if a.valid {
				aliasCount++
			}
		}

		// Note: This doesn't currently take into account that we have two types of aliasing.
		isCopy := removeCount < aliasCount

		start := int32(len(d.tape))
		if isCopy {
			d.enterCopy(primaryIdx)
		} else {
			d.enterBlank(primaryIdx)
		}
		size := 2

		if isCopy && removeCount > 0 {
			// Delete fields we don't need (in key order, for deterministic patches).
			rit := d.right.Iter(idx)
			for lit := d.left.Iter(int(primaryIdx)); !lit.IsDone(); lit.Next() {
				lk := lit.GetKey()
				for !rit.IsDone() && rit.GetKey() < lk {
					rit.Next()
				}
				if rit.IsDone() || rit.GetKey() != lk {
					d.emit(opRec{code: codeObjectDeleteField, a: int32(lit.GetEntry().Reference.Index)})
					size += 2
				}
			}
		}

		for it := d.right.Iter(idx); !it.IsDone(); it.Next() {
			fieldEntry := it.GetEntry()
			fieldKey := fieldEntry.Reference.Key
			pos := fieldEntry.Reference.Index

			if alias := d.mapAlias[cand.aliasOff+int32(pos)]; alias.valid {
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
				continue
			}

			didPatch := false
			if ri := d.findRequest(d.ranges[rg+pos], d.ranges[rg+pos+1], cand.contextIdx); ri >= 0 {
				fieldReq := &d.reqs[ri]
				if fieldReq.hasPatch {
					d.emit(opRec{code: codeInclude, a: fieldReq.patch.start, b: fieldReq.patch.end})
					size += fieldReq.size
					if fieldReq.outputKey == fieldKey {
						d.emit(opRec{code: codeReturnIntoObjectSameKeyPop})
						size += 1
					} else {
						d.emit(opRec{code: codeReturnIntoObjectPop, key: fieldKey})
						size += 1 + len(fieldKey)
					}
					didPatch = true
				}
			}

			if !didPatch {
				d.emit(opRec{code: codeObjectSetFieldValue, val: fieldEntry.Value, key: fieldKey})
				size += 1 + len(fieldKey) + fieldEntry.Size
			}
		}

		d.update(cand.requestIdx, span{start, int32(len(d.tape))}, size, d.left.Entries[primaryIdx].Reference.Key)
	}

	clear(d.reqs[reqMark:])
	d.reqs = d.reqs[:reqMark]
	d.mapCands = d.mapCands[:candMark]
	d.mapAlias = d.mapAlias[:aliasMark]
	d.ranges = d.ranges[:rangeMark]
}

// Slices

func (d *differ) insertSliceAlias(cand *sliceCandidate, target, source *mendoza.Reference) {
	// We assume here that you'll only invoke this method in the same order
	// as you want to build the array.
	al := d.sliceAlias[cand.aliasOff:]
	pos := target.Index
	src := int32(source.Index)
	cur := &al[pos]
	ok := cur.valid

	if ok && cur.prevIsAdjacent {
		// Once we've found something which is adjacent. Don't look any further.
		return
	}

	if pos > 0 && al[pos-1].valid {
		prev := &al[pos-1]
		if prev.elementIdx+1 == src {
			// This one is perfectly adjacent. Use it!
			*cur = sliceAlias{elementIdx: src, prevIsAdjacent: true, valid: true}
			prev.nextIsAdjacent = true
			return
		}
		if src <= prev.elementIdx {
			// We want to prefer values that are _after_ the previous index.
			if ok {
				// However, we can only return if we've already found a value.
				return
			}
		}
	}

	if ok && cur.elementIdx < src {
		// Prefer smaller over larger
		return
	}

	*cur = sliceAlias{elementIdx: src, valid: true}
}

func (d *differ) reconstructSlice(idx int, off, n int32) {
	entry := &d.right.Entries[idx]
	k := len(entry.Value.([]interface{}))

	reqMark := len(d.reqs)
	candMark := len(d.sliceCands)
	aliasMark := len(d.sliceAlias)
	rangeMark := len(d.ranges)

	for i := off; i < off+n; i++ {
		req := &d.reqs[i]
		if !d.left.Entries[req.primaryIdx].IsNonEmptySlice() {
			continue
		}
		d.sliceCands = append(d.sliceCands, sliceCandidate{
			contextIdx: req.primaryIdx,
			requestIdx: i,
			aliasOff:   int32(len(d.sliceAlias)),
		})
		d.sliceAlias = grow(d.sliceAlias, k)
	}

	kidsMark := len(d.kids)
	for c := candMark; c < len(d.sliceCands); c++ {
		cand := &d.sliceCands[c]
		cand.kidsOff, cand.kidsEnd = d.indexKids(cand.contextIdx)
	}
	for it := d.right.Iter(idx); !it.IsDone(); it.Next() {
		elementEntry := it.GetEntry()
		for c := candMark; c < len(d.sliceCands); c++ {
			cand := &d.sliceCands[c]
			lo, hi := d.matchKids(cand.kidsOff, cand.kidsEnd, elementEntry.Hash)
			for ; lo < hi; lo++ {
				other := &d.left.Entries[d.kids[lo].idx]
				d.insertSliceAlias(cand, &elementEntry.Reference, &other.Reference)
			}
		}
	}
	d.kids = d.kids[:kidsMark]

	pMark := len(d.pend)
	for c := candMark; c < len(d.sliceCands); c++ {
		cand := d.sliceCands[c]
		contextIter := d.left.Iter(int(cand.contextIdx))

		for it := d.right.Iter(idx); !it.IsDone(); it.Next() {
			if contextIter.IsDone() {
				break
			}
			elementEntry := it.GetEntry()
			pos := elementEntry.Reference.Index
			if !d.sliceAlias[cand.aliasOff+int32(pos)].valid {
				d.pend = append(d.pend, pendingReq{
					field: int32(pos),
					req: request{
						contextIdx: cand.contextIdx,
						primaryIdx: int32(contextIter.GetIndex()),
						size:       elementEntry.Size + 1,
					},
				})
			}
			contextIter.Next()
		}
	}
	rg := d.placeRequests(pMark, k)

	for it := d.right.Iter(idx); !it.IsDone(); it.Next() {
		pos := it.GetEntry().Reference.Index
		start, end := d.ranges[rg+pos], d.ranges[rg+pos+1]
		d.reconstruct(it.GetIndex(), start, end-start)
	}

	for c := candMark; c < len(d.sliceCands); c++ {
		cand := d.sliceCands[c]
		contextIdx := cand.contextIdx

		start := int32(len(d.tape))
		d.enterBlank(contextIdx)
		size := 2

		startSlice := int32(-1)

		for it := d.right.Iter(idx); !it.IsDone(); it.Next() {
			elementEntry := it.GetEntry()
			pos := elementEntry.Reference.Index

			if alias := d.sliceAlias[cand.aliasOff+int32(pos)]; alias.valid {
				if startSlice == -1 {
					startSlice = alias.elementIdx
				}
				if !alias.nextIsAdjacent {
					d.emit(opRec{code: codeArrayAppendSlice, a: startSlice, b: alias.elementIdx + 1})
					size += 3
					startSlice = -1
				}
				continue
			}

			didPatch := false
			if ri := d.findRequest(d.ranges[rg+pos], d.ranges[rg+pos+1], contextIdx); ri >= 0 {
				elementReq := &d.reqs[ri]
				if elementReq.hasPatch {
					d.emit(opRec{code: codeInclude, a: elementReq.patch.start, b: elementReq.patch.end})
					size += elementReq.size
					d.emit(opRec{code: codeReturnIntoArrayPop})
					size += 1
					didPatch = true
				}
			}

			if !didPatch {
				d.emit(opRec{code: codeArrayAppendValue, val: elementEntry.Value})
				size += 1 + elementEntry.Size
			}
		}

		d.update(cand.requestIdx, span{start, int32(len(d.tape))}, size, d.left.Entries[contextIdx].Reference.Key)
	}

	clear(d.reqs[reqMark:])
	d.reqs = d.reqs[:reqMark]
	d.sliceCands = d.sliceCands[:candMark]
	d.sliceAlias = d.sliceAlias[:aliasMark]
	d.ranges = d.ranges[:rangeMark]
}

// Strings

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

func (d *differ) reconstructString(rightString string, off, n int32) {
	for i := off; i < off+n; i++ {
		primaryIdx := d.reqs[i].primaryIdx
		leftEntry := &d.left.Entries[primaryIdx]

		leftString, ok := leftEntry.Value.(string)
		if !ok {
			continue
		}
		if leftString == rightString {
			panic("unnecessary reconstruction of string")
		}

		start := int32(len(d.tape))
		d.enterBlank(primaryIdx)
		size := 2

		prefix := commonPrefix(leftString, rightString)
		if prefix > 0 {
			d.emit(opRec{code: codeStringAppendSlice, a: 0, b: int32(prefix)})
			size += 3
		}

		suffix := commonSuffix(leftString, rightString, prefix)

		mid := rightString[prefix : len(rightString)-suffix]
		if len(mid) > 0 {
			d.emit(opRec{code: codeStringAppendString, key: mid})
			size += 1 + len(mid)
		}

		if suffix > 0 {
			d.emit(opRec{code: codeStringAppendSlice, a: int32(len(leftString) - suffix), b: int32(len(leftString))})
			size += 3
		}

		d.update(i, span{start, int32(len(d.tape))}, size, leftEntry.Reference.Key)
	}
}

// Output

// walk calls fn for every op in sp, expanding includes. It uses an explicit
// stack owned by the Differ so it does not allocate in steady state.
func (d *Differ) walk(sp span, fn func(op *opRec) error) error {
	stack := append(d.stack[:0], sp)
	tape := d.st.tape
	for len(stack) > 0 {
		top := len(stack) - 1
		if stack[top].start == stack[top].end {
			stack = stack[:top]
			continue
		}
		op := &tape[stack[top].start]
		stack[top].start++
		if op.code == codeInclude {
			stack = append(stack, span{op.a, op.b})
			continue
		}
		if err := fn(op); err != nil {
			d.stack = stack[:0]
			return err
		}
	}
	d.stack = stack[:0]
	return nil
}

func (d *Differ) writeSpan(w Writer, sp span) error {
	return d.walk(sp, func(op *opRec) error {
		if err := w.WriteUint8(op.code); err != nil {
			return err
		}
		switch op.code {
		case codeValue, codeArrayAppendValue:
			return w.WriteValue(op.val)
		case codePushFieldBlank, codePushFieldCopy, codePushElementBlank, codePushElementCopy,
			codeObjectCopyField, codeObjectDeleteField:
			return w.WriteUint(int(op.a))
		case codeReturnIntoObjectPop, codeStringAppendString:
			return w.WriteString(op.key)
		case codeObjectSetFieldValue:
			if err := w.WriteValue(op.val); err != nil {
				return err
			}
			return w.WriteString(op.key)
		case codeArrayAppendSlice, codeStringAppendSlice:
			if err := w.WriteUint(int(op.a)); err != nil {
				return err
			}
			return w.WriteUint(int(op.b))
		}
		return nil // codeBlank, codeReturnIntoObjectSameKeyPop, codeReturnIntoArrayPop
	})
}

// opSlabs backs the ops of a materialized Patch: one allocation per op type
// instead of one per op.
type opSlabs struct {
	value          []OpValue
	pushFieldBlank []OpPushFieldBlank
	pushFieldCopy  []OpPushFieldCopy
	pushElemBlank  []OpPushElementBlank
	pushElemCopy   []OpPushElementCopy
	copyField      []OpObjectCopyField
	deleteField    []OpObjectDeleteField
	returnPop      []OpReturnIntoObjectPop
	setField       []OpObjectSetFieldValue
	appendSlice    []OpArrayAppendSlice
	appendValue    []OpArrayAppendValue
	strString      []OpStringAppendString
	strSlice       []OpStringAppendSlice
}

func next[T any](s *[]T) *T {
	p := &(*s)[0]
	*s = (*s)[1:]
	return p
}

func (d *Differ) materialize(sp span) Patch {
	var counts [256]int
	total := 0
	_ = d.walk(sp, func(op *opRec) error {
		counts[op.code]++
		total++
		return nil
	})

	var s opSlabs
	if n := counts[codeValue]; n > 0 {
		s.value = make([]OpValue, n)
	}
	if n := counts[codePushFieldBlank]; n > 0 {
		s.pushFieldBlank = make([]OpPushFieldBlank, n)
	}
	if n := counts[codePushFieldCopy]; n > 0 {
		s.pushFieldCopy = make([]OpPushFieldCopy, n)
	}
	if n := counts[codePushElementBlank]; n > 0 {
		s.pushElemBlank = make([]OpPushElementBlank, n)
	}
	if n := counts[codePushElementCopy]; n > 0 {
		s.pushElemCopy = make([]OpPushElementCopy, n)
	}
	if n := counts[codeObjectCopyField]; n > 0 {
		s.copyField = make([]OpObjectCopyField, n)
	}
	if n := counts[codeObjectDeleteField]; n > 0 {
		s.deleteField = make([]OpObjectDeleteField, n)
	}
	if n := counts[codeReturnIntoObjectPop]; n > 0 {
		s.returnPop = make([]OpReturnIntoObjectPop, n)
	}
	if n := counts[codeObjectSetFieldValue]; n > 0 {
		s.setField = make([]OpObjectSetFieldValue, n)
	}
	if n := counts[codeArrayAppendSlice]; n > 0 {
		s.appendSlice = make([]OpArrayAppendSlice, n)
	}
	if n := counts[codeArrayAppendValue]; n > 0 {
		s.appendValue = make([]OpArrayAppendValue, n)
	}
	if n := counts[codeStringAppendString]; n > 0 {
		s.strString = make([]OpStringAppendString, n)
	}
	if n := counts[codeStringAppendSlice]; n > 0 {
		s.strSlice = make([]OpStringAppendSlice, n)
	}

	patch := make(Patch, 0, total)
	_ = d.walk(sp, func(op *opRec) error {
		var o Op
		switch op.code {
		case codeValue:
			p := next(&s.value)
			p.Value = op.val
			o = p
		case codeBlank:
			o = &OpBlank{}
		case codePushFieldBlank:
			p := next(&s.pushFieldBlank)
			p.Index = int(op.a)
			o = p
		case codePushFieldCopy:
			p := next(&s.pushFieldCopy)
			p.Index = int(op.a)
			o = p
		case codePushElementBlank:
			p := next(&s.pushElemBlank)
			p.Index = int(op.a)
			o = p
		case codePushElementCopy:
			p := next(&s.pushElemCopy)
			p.Index = int(op.a)
			o = p
		case codeObjectCopyField:
			p := next(&s.copyField)
			p.Index = int(op.a)
			o = p
		case codeObjectDeleteField:
			p := next(&s.deleteField)
			p.Index = int(op.a)
			o = p
		case codeReturnIntoObjectPop:
			p := next(&s.returnPop)
			p.Key = op.key
			o = p
		case codeReturnIntoObjectSameKeyPop:
			o = &OpReturnIntoObjectSameKeyPop{}
		case codeReturnIntoArrayPop:
			o = &OpReturnIntoArrayPop{}
		case codeObjectSetFieldValue:
			p := next(&s.setField)
			p.Value = op.val
			p.Key = op.key
			o = p
		case codeArrayAppendSlice:
			p := next(&s.appendSlice)
			p.Left, p.Right = int(op.a), int(op.b)
			o = p
		case codeArrayAppendValue:
			p := next(&s.appendValue)
			p.Value = op.val
			o = p
		case codeStringAppendString:
			p := next(&s.strString)
			p.String = op.key
			o = p
		case codeStringAppendSlice:
			p := next(&s.strSlice)
			p.Left, p.Right = int(op.a), int(op.b)
			o = p
		}
		patch = append(patch, o)
		return nil
	})
	return patch
}
