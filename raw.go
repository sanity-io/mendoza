package mendoza

import (
	"bytes"
	"errors"
	"math"
	"sort"
	"strconv"
	"sync"
)

// ErrInvalidJSON is returned by RawPatcher when the document or the patch
// is not well formed JSON.
var ErrInvalidJSON = errors.New("mendoza: invalid JSON")

// RawPatcher applies JSON encoded patches to JSON encoded documents without
// decoding them.
//
// ApplyPatch needs the document as map[string]interface{} and the patch as
// a []Op, so a caller holding bytes pays for json.Unmarshal of both, the
// patch, and json.Marshal of the result. RawPatcher runs the patch straight
// off its bytes and keeps the document as bytes: the input stack holds
// spans of the document, and the output is a rope of spans of the document
// and the patch, written out once at the end. Values the patch copies are
// never decoded; in a typical patch that is nearly the whole document.
//
// Only the objects and arrays the patch steps into are indexed, and each
// one only once. A RawPatcher keeps its buffers between calls, so once warm
// Apply allocates nothing beyond growing dst. It is not safe for concurrent
// use; ApplyRawPatch uses a pool.
//
// The result equals the result of ApplyPatch as JSON, but not byte for
// byte: values copied from the document keep their formatting and number
// representation (so integers beyond 2^53 stay exact), objects the patch
// changes are written with their keys sorted, and strings are escaped only
// as much as JSON requires. The document and patch must be valid JSON:
// like gjson, RawPatcher skips over values without validating them, and
// copies them as they are. Malformed input gives an error or malformed
// output, never a panic.
type RawPatcher struct {
	doc, patch []byte
	scratch    []byte // escaped substrings; starts with "", the empty key

	pos int   // read offset in patch
	err error // the first error; after one, results are discarded

	in      []input  // the input stack
	out     []ref    // the output stack
	nodes   []node   // output values being changed
	items   []item   // their contents, as linked lists
	members []member // indexed objects and arrays
	kids    []kid    // recorded while skipping; see kid
	changes []item   // emitObject's sorted changes

	ka, kb, str []byte // decode buffers

	memberSort memberSorter
	changeSort changeSorter
}

var rawPatchers = sync.Pool{New: func() interface{} { return new(RawPatcher) }}

// ApplyRawPatch applies a JSON encoded patch to a JSON encoded document and
// returns the JSON encoded result. See RawPatcher.
func ApplyRawPatch(doc, patch []byte) ([]byte, error) {
	p := rawPatchers.Get().(*RawPatcher)
	defer rawPatchers.Put(p)
	return p.Apply(nil, doc, patch)
}

// Apply appends to dst the result of applying patch to doc. dst must not
// overlap doc or patch.
func (p *RawPatcher) Apply(dst, doc, patch []byte) ([]byte, error) {
	p.reset(doc, patch)
	defer p.release()

	root, ok := rootSpan(doc)
	if !ok {
		return dst, ErrInvalidJSON
	}
	p.in = append(p.in, input{val: root, key: emptyKey})
	p.push(p.docRef(root))
	p.run()
	if p.err != nil {
		return dst, p.err
	}
	out := p.emit(dst, p.out[len(p.out)-1])
	if p.err != nil { // emitting indexes objects the patch changed
		return dst, p.err
	}
	return out, nil
}

func (p *RawPatcher) reset(doc, patch []byte) {
	p.doc, p.patch = doc, patch
	p.scratch = append(p.scratch[:0], `""`...)
	p.pos, p.err = 0, nil
	p.in = p.in[:0]
	p.out = p.out[:0]
	p.nodes = p.nodes[:0]
	p.items = p.items[:0]
	p.members = p.members[:0]
	p.kids = p.kids[:0]
	p.changes = p.changes[:0]
}

// release drops the caller's slices so a pooled RawPatcher doesn't keep
// them alive.
func (p *RawPatcher) release() {
	p.doc, p.patch = nil, nil
}

func (p *RawPatcher) fail(err error) {
	if p.err == nil {
		p.err = err
	}
}

// rootSpan returns the span of the document's value.
func rootSpan(doc []byte) (span, bool) {
	s := skipWS(doc, 0)
	e := trimEnd(doc, s, len(doc))
	if s == e {
		return span{}, false
	}
	switch doc[s] {
	case '{', '[':
		// Don't scan the whole document just to find its end: the patch
		// scans what it needs. Check the brackets match.
		return span{s, e}, e-s >= 2 && doc[e-1] == doc[s]+2
	}
	return span{s, e}, skipValue(doc, s) == e
}

// Values.

type span struct{ start, end int }

// Buffers a ref or key can point into.
const (
	bufDoc uint8 = iota
	bufPatch
	bufScratch
)

func (p *RawPatcher) bytesOf(buf uint8) []byte {
	switch buf {
	case bufDoc:
		return p.doc
	case bufPatch:
		return p.patch
	default:
		return p.scratch
	}
}

// Kinds of ref.
const (
	refNull uint8 = iota // null, or Blank
	refRaw               // JSON bytes: buf[start:end]
	refNode              // a value being changed: nodes[start]
)

// A ref is an output value, or a piece of one: a whole value, the elements
// of an array, or the escaped content of a string.
type ref struct {
	kind, buf  uint8
	start, end int
}

// docRef returns the ref for a value in the document. ApplyPatch treats a
// nil source like Blank, so null is refNull.
func (p *RawPatcher) docRef(v span) ref {
	if p.doc[v.start] == 'n' {
		return ref{}
	}
	return ref{kind: refRaw, buf: bufDoc, start: v.start, end: v.end}
}

// A key is a JSON string, quotes included.
type key struct {
	buf        uint8
	esc        bool // has escapes
	start, end int
	pre        uint64 // the first 8 bytes of its decoded content, big-endian
}

// emptyKey is the key of the root and of array elements, as in ApplyPatch.
var emptyKey = key{buf: bufScratch, start: 0, end: 2}

// A member is a value directly inside an object or array: an object's
// field, with its key, or an array's element.
type member struct {
	key  key
	val  span // in the container's buffer
	kids span // val's kids in p.kids, if recorded
}

// A kid is a string or container directly inside a document container that
// was skipped over: skipping stops at those anyway, so recording them is
// nearly free, and indexing the container later jumps from kid to kid
// instead of scanning it again. Numbers and literals are not recorded;
// indexing scans the gaps between kids for them.
type kid struct {
	span
	esc bool // strings: has escapes
}

// An input is an entry on the input stack: a value in the document.
type input struct {
	val     span
	key     key  // the key it was pushed from
	kids    span // val's kids, if recorded
	members span // val's members, once indexed
	indexed bool
}

// Slots of a node.
const (
	slotObject uint8 = iota
	slotArray
	slotString
)

// A node is an output value the patch is changing. It mirrors ApplyPatch's
// outputEntry: a source plus writable object, array and string slots, of
// which the result is the first set in that order, else the source. A
// patch from the differ only ever uses one slot.
type node struct {
	src     ref  // the array and string slots take it over
	object  bool // the object slot is set; it starts from src, if any
	array   bool // the array slot is set, as a non-nil slice
	members span // src's members, once indexed
	indexed bool
	first   int // items, linked through item.next; -1 if none
	last    int
}

// An item is a slot's content: a field set or deleted on the object, an
// element of the array, or a piece of the string.
type item struct {
	slot uint8
	del  bool
	key  key
	val  ref
	next int
}

// Reading the patch.

// run executes the patch: a flat JSON array of opcodes and their params,
// read and executed one op at a time.
func (p *RawPatcher) run() {
	b := p.patch
	i := skipWS(b, 0)
	if i >= len(b) || b[i] != '[' {
		p.fail(ErrInvalidJSON)
		return
	}
	p.pos = skipWS(b, i+1)
	for p.pos >= len(b) || b[p.pos] != ']' {
		p.exec(p.readUint())
		if p.err != nil {
			return
		}
		if p.pos = skipWS(b, p.pos); p.pos < len(b) && b[p.pos] == ']' {
			break
		}
		p.comma()
	}
	if skipWS(b, p.pos+1) != len(b) {
		p.fail(ErrInvalidJSON)
	}
}

// comma moves past the comma before the next value in the patch.
func (p *RawPatcher) comma() {
	b := p.patch
	i := skipWS(b, p.pos)
	if i >= len(b) || b[i] != ',' {
		p.fail(ErrInvalidJSON)
		return
	}
	p.pos = skipWS(b, i+1)
}

func (p *RawPatcher) readUint() int {
	b := p.patch
	i, n := p.pos, 0
	for i < len(b) && '0' <= b[i] && b[i] <= '9' && n <= math.MaxInt32 {
		n = n*10 + int(b[i]-'0')
		i++
	}
	if i == p.pos || i < len(b) && !scalarEnd[b[i]] {
		return p.readUintSlow()
	}
	p.pos = i
	return n
}

// readUintSlow accepts integers written as floats, such as 1.0 or 1e2, as
// ReadUintFromValueReader does.
func (p *RawPatcher) readUintSlow() int {
	e := skipValue(p.patch, p.pos)
	if e < 0 {
		p.fail(ErrInvalidJSON)
		return 0
	}
	f, err := strconv.ParseFloat(string(p.patch[p.pos:e]), 64)
	if err != nil || f < 0 || f > math.MaxInt32 || f != math.Trunc(f) {
		p.fail(ErrInvalidPatch)
		return 0
	}
	p.pos = e
	return int(f)
}

func (p *RawPatcher) paramUint() int {
	p.comma()
	return p.readUint()
}

func (p *RawPatcher) paramKey() key {
	p.comma()
	b := p.patch
	if p.pos >= len(b) || b[p.pos] != '"' {
		p.fail(ErrInvalidPatch)
		return emptyKey
	}
	e, esc := skipString(b, p.pos)
	if e < 0 {
		p.fail(ErrInvalidJSON)
		return emptyKey
	}
	k := p.newKey(bufPatch, esc, p.pos, e)
	p.pos = e
	return k
}

func (p *RawPatcher) paramValue() ref {
	p.comma()
	e := skipValue(p.patch, p.pos)
	if e < 0 {
		p.fail(ErrInvalidJSON)
		return ref{}
	}
	r := ref{kind: refRaw, buf: bufPatch, start: p.pos, end: e}
	if p.patch[p.pos] == 'n' {
		r = ref{} // null behaves like Blank, as a nil source does in ApplyPatch
	}
	p.pos = e
	return r
}

// Executing ops.

// exec executes one op, reading its params. Ops record a failure in p.err
// and carry on; run stops after the op, and the result is discarded.
func (p *RawPatcher) exec(op int) {
	if op > math.MaxUint8 {
		p.fail(ErrInvalidPatch)
		return
	}
	switch uint8(op) {
	case codeValue:
		p.push(p.paramValue())
	case codeCopy:
		p.copyInput()
	case codeBlank:
		p.push(ref{})
	case codeReturnIntoArray:
		p.appendElement(p.pop())
	case codeReturnIntoObject:
		p.returnIntoObject(p.paramKey())
	case codeReturnIntoObjectSameKey:
		p.returnIntoObject(p.inputKey())
	case codePushField:
		p.pushField(p.paramUint())
	case codePushElement:
		p.pushElement(p.paramUint())
	case codePushParent:
		p.pushParent(p.paramUint())
	case codePop:
		p.popInput()
	case codePushFieldCopy:
		p.pushField(p.paramUint())
		p.copyInput()
	case codePushFieldBlank:
		p.pushField(p.paramUint())
		p.push(ref{})
	case codePushElementCopy:
		p.pushElement(p.paramUint())
		p.copyInput()
	case codePushElementBlank:
		p.pushElement(p.paramUint())
		p.push(ref{})
	case codeReturnIntoObjectPop:
		p.returnIntoObject(p.paramKey())
		p.popInput()
	case codeReturnIntoObjectSameKeyPop:
		p.returnIntoObject(p.inputKey())
		p.popInput()
	case codeReturnIntoArrayPop:
		p.appendElement(p.pop())
		p.popInput()
	case codeObjectSetFieldValue:
		p.push(p.paramValue())
		p.returnIntoObject(p.paramKey())
	case codeObjectCopyField:
		p.pushField(p.paramUint())
		p.copyInput()
		p.returnIntoObject(p.inputKey())
		p.popInput()
	case codeObjectDeleteField:
		p.deleteField(p.paramUint())
	case codeArrayAppendValue:
		p.appendElement(p.paramValue())
	case codeArrayAppendSlice:
		l := p.paramUint()
		p.appendSlice(l, p.paramUint())
	case codeStringAppendString:
		k := p.paramKey()
		p.appendChars(ref{kind: refRaw, buf: bufPatch, start: k.start + 1, end: k.end - 1})
	case codeStringAppendSlice:
		l := p.paramUint()
		p.appendSubstring(l, p.paramUint())
	default:
		p.fail(ErrInvalidPatch)
	}
}

// The input stack.

// input returns the input value, or nil if the stack is empty.
func (p *RawPatcher) input() *input {
	if len(p.in) == 0 {
		p.fail(ErrInvalidPatch)
		return nil
	}
	return &p.in[len(p.in)-1]
}

func (p *RawPatcher) inputKey() key {
	if in := p.input(); in != nil {
		return in.key
	}
	return emptyKey
}

func (p *RawPatcher) pushField(n int) {
	if m, ok := p.inputMember(n, '{'); ok {
		p.in = append(p.in, input{val: m.val, key: m.key, kids: m.kids})
	}
}

func (p *RawPatcher) pushElement(n int) {
	if m, ok := p.inputMember(n, '['); ok {
		p.in = append(p.in, input{val: m.val, key: emptyKey, kids: m.kids})
	}
}

func (p *RawPatcher) pushParent(n int) {
	i := len(p.in) - 2 - n
	if i < 0 {
		p.fail(ErrInvalidPatch)
		return
	}
	p.in = append(p.in, p.in[i])
}

func (p *RawPatcher) popInput() {
	if p.input() != nil {
		p.in = p.in[:len(p.in)-1]
	}
}

// inputMember returns member n of the input value, an object if open is
// '{', an array if '['. An object's members are in key order.
func (p *RawPatcher) inputMember(n int, open byte) (member, bool) {
	ms, ok := p.inputMembers(open)
	if !ok || n >= ms.end-ms.start {
		p.fail(ErrInvalidPatch)
		return member{}, false
	}
	return p.members[ms.start+n], true
}

func (p *RawPatcher) inputMembers(open byte) (span, bool) {
	in := p.input()
	if in == nil {
		return span{}, false
	}
	if p.doc[in.val.start] != open {
		p.fail(ErrInvalidPatch)
		return span{}, false
	}
	if !in.indexed {
		in.members, in.indexed = p.index(bufDoc, in.val, in.kids), true
	}
	return in.members, p.err == nil
}

// The output stack.

func (p *RawPatcher) push(r ref) {
	p.out = append(p.out, r)
}

// pop removes and returns the output value. The root can't be popped.
func (p *RawPatcher) pop() ref {
	if len(p.out) < 2 {
		p.fail(ErrInvalidPatch)
		return ref{}
	}
	r := p.out[len(p.out)-1]
	p.out = p.out[:len(p.out)-1]
	return r
}

func (p *RawPatcher) copyInput() {
	if in := p.input(); in != nil {
		p.push(p.docRef(in.val))
	}
}

func (p *RawPatcher) returnIntoObject(k key) {
	v := p.pop()
	if n := p.outputObject(); n >= 0 {
		p.addItem(n, item{slot: slotObject, key: k, val: v})
	}
}

func (p *RawPatcher) deleteField(i int) {
	m, ok := p.inputMember(i, '{')
	if !ok {
		return
	}
	if n := p.outputObject(); n >= 0 {
		p.addItem(n, item{slot: slotObject, key: m.key, del: true})
	}
}

func (p *RawPatcher) appendElement(v ref) {
	if n := p.outputArray(); n >= 0 {
		p.nodes[n].array = true
		p.addItem(n, item{slot: slotArray, val: v})
	}
}

// appendSlice appends elements [l, r) of the input array as one piece: the
// bytes from the first to the last, commas included.
func (p *RawPatcher) appendSlice(l, r int) {
	ms, ok := p.inputMembers('[')
	if !ok {
		return
	}
	if l > r || r > ms.end-ms.start {
		p.fail(ErrInvalidPatch)
		return
	}
	n := p.outputArray()
	if n < 0 || l == r {
		return
	}
	p.nodes[n].array = true
	first, last := p.members[ms.start+l].val, p.members[ms.start+r-1].val
	p.addItem(n, item{slot: slotArray, val: ref{kind: refRaw, buf: bufDoc, start: first.start, end: last.end}})
}

// appendSubstring appends bytes [l, r) of the input string. The offsets are
// into the decoded string, which is the content itself unless it has
// escapes.
func (p *RawPatcher) appendSubstring(l, r int) {
	in := p.input()
	if in == nil {
		return
	}
	if p.doc[in.val.start] != '"' {
		p.fail(ErrInvalidPatch)
		return
	}
	start, end := in.val.start+1, in.val.end-1
	content := p.doc[start:end]
	if bytes.IndexByte(content, '\\') < 0 {
		if l > r || r > len(content) {
			p.fail(ErrInvalidPatch)
			return
		}
		p.appendChars(ref{kind: refRaw, buf: bufDoc, start: start + l, end: start + r})
		return
	}
	p.str = unescape(p.str[:0], content)
	if l > r || r > len(p.str) {
		p.fail(ErrInvalidPatch)
		return
	}
	s := len(p.scratch)
	p.scratch = escape(p.scratch, p.str[l:r])
	p.appendChars(ref{kind: refRaw, buf: bufScratch, start: s, end: len(p.scratch)})
}

func (p *RawPatcher) appendChars(c ref) {
	if n := p.outputString(); n >= 0 && c.end > c.start {
		p.addItem(n, item{slot: slotString, val: c})
	}
}

// outputNode makes the output value a node, if it is not one already, and
// returns its index.
func (p *RawPatcher) outputNode() int {
	t := &p.out[len(p.out)-1]
	if t.kind == refNode {
		return t.start
	}
	i := len(p.nodes)
	p.nodes = append(p.nodes, node{src: *t, first: -1, last: -1})
	*t = ref{kind: refNode, start: i}
	return i
}

// srcType returns the first byte of the node's source, or 0 if it has none.
func (p *RawPatcher) srcType(n *node) byte {
	if n.src.kind != refRaw {
		return 0
	}
	return p.bytesOf(n.src.buf)[n.src.start]
}

// outputObject sets the output value's object slot and returns its node,
// or -1, as patcher.outputObject: the source, if any, must be an object,
// and stays.
func (p *RawPatcher) outputObject() int {
	i := p.outputNode()
	n := &p.nodes[i]
	if n.object {
		return i
	}
	if t := p.srcType(n); t != 0 && t != '{' {
		p.fail(ErrInvalidPatch)
		return -1
	}
	n.object = true
	// The source is often the input value, already indexed.
	if n.src.kind == refRaw && n.src.buf == bufDoc && len(p.in) > 0 {
		if in := &p.in[len(p.in)-1]; in.indexed && in.val.start == n.src.start {
			n.members, n.indexed = in.members, true
		}
	}
	return i
}

func (p *RawPatcher) outputArray() int  { return p.takeSource('[', slotArray) }
func (p *RawPatcher) outputString() int { return p.takeSource('"', slotString) }

// takeSource prepares the output value's array or string slot and returns
// its node, or -1, as patcher.outputArray and outputString: the source, if
// any, must be of that type, and its content becomes the slot's first item.
func (p *RawPatcher) takeSource(open byte, slot uint8) int {
	i := p.outputNode()
	n := &p.nodes[i]
	switch p.srcType(n) {
	case 0:
		return i
	case open:
	default:
		p.fail(ErrInvalidPatch)
		return -1
	}
	src := n.src
	n.src = ref{}
	b := p.bytesOf(src.buf)
	s, e := src.start+1, src.end-1 // inside the brackets or quotes
	if slot == slotArray {
		n.array = true
		s = skipWS(b, s)
		e = trimEnd(b, s, e)
	}
	if e > s {
		p.addItem(i, item{slot: slot, val: ref{kind: refRaw, buf: src.buf, start: s, end: e}})
	}
	return i
}

func (p *RawPatcher) addItem(n int, it item) {
	it.next = -1
	i := len(p.items)
	p.items = append(p.items, it)
	nd := &p.nodes[n]
	if nd.last < 0 {
		nd.first = i
	} else {
		p.items[nd.last].next = i
	}
	nd.last = i
}

// Indexing.

// index appends the members of the object or array at buf[v] to p.members
// and returns their span. An object's are sorted by key, keeping the last
// of any duplicates: the order and content ApplyPatch sees in a decoded
// map. kids are v's kids, if recorded.
func (p *RawPatcher) index(buf uint8, v span, kids span) span {
	b := p.bytesOf(buf)
	first := len(p.members)
	object := b[v.start] == '{'
	closer := b[v.start] + 2 // '}' or ']'
	record := buf == bufDoc
	sorted := true

	i := skipWS(b, v.start+1)
	if i < v.end && b[i] == closer {
		return span{first, first}
	}
	for {
		var m member
		if object {
			if i >= v.end || b[i] != '"' {
				break
			}
			e, esc, _ := p.child(b, i, &kids, false)
			if e < 0 {
				break
			}
			m.key = p.newKey(buf, esc, i, e)
			if i = skipWS(b, e); i >= v.end || b[i] != ':' {
				break
			}
			i = skipWS(b, i+1)
		}
		e, _, k := p.child(b, i, &kids, record)
		if e < 0 || e > v.end {
			break
		}
		m.val, m.kids = span{i, e}, k
		if object && sorted && len(p.members) > first && p.compareKeys(p.members[len(p.members)-1].key, m.key) >= 0 {
			sorted = false
		}
		p.members = append(p.members, m)

		if i = skipWS(b, e); i < v.end && b[i] == closer {
			if !sorted {
				p.sortMembers(first)
			}
			return span{first, len(p.members)}
		}
		if i >= v.end || b[i] != ',' {
			break
		}
		i = skipWS(b, i+1)
	}
	p.fail(ErrInvalidJSON)
	return span{first, first}
}

// sortMembers sorts p.members[first:] stably by key, then drops all but the
// last of each run of equal keys.
func (p *RawPatcher) sortMembers(first int) {
	ms := p.members[first:]
	p.memberSort = memberSorter{p, ms}
	sort.Stable(&p.memberSort)
	p.memberSort = memberSorter{}
	w := 0
	for j := range ms {
		if j+1 < len(ms) && p.compareKeys(ms[j].key, ms[j+1].key) == 0 {
			continue
		}
		ms[w] = ms[j]
		w++
	}
	p.members = p.members[:first+w]
}

// child returns the end of the value at b[i], whether it is a string with
// escapes, and, if record is set and it is a container, its kids. A kid at
// the front of kids gives the end without scanning.
func (p *RawPatcher) child(b []byte, i int, kids *span, record bool) (int, bool, span) {
	if kids.start < kids.end {
		if k := p.kids[kids.start]; k.start == i {
			kids.start++
			return k.end, k.esc, span{}
		}
	}
	if i < len(b) {
		switch b[i] {
		case '"':
			e, esc := skipString(b, i)
			return e, esc, span{}
		case '{', '[':
			if record {
				return p.skipRecord(b, i)
			}
		}
	}
	return skipValue(b, i), false, span{}
}

// skipRecord is skipContainer, recording the container's kids.
func (p *RawPatcher) skipRecord(b []byte, i int) (int, bool, span) {
	first := len(p.kids)
	depth, open := 0, 0
	for i < len(b) {
		c := b[i]
		if !structural[c] {
			i++
			continue
		}
		switch c {
		case '"':
			e, esc := skipString(b, i)
			if e < 0 {
				return -1, false, span{}
			}
			if depth == 1 {
				p.kids = append(p.kids, kid{span{i, e}, esc})
			}
			i = e
			continue
		case '{', '[':
			if depth++; depth == 2 {
				open = i
			}
		default: // '}', ']'
			if depth--; depth == 1 {
				p.kids = append(p.kids, kid{span: span{open, i + 1}})
			} else if depth == 0 {
				return i + 1, false, span{first, len(p.kids)}
			}
		}
		i++
	}
	return -1, false, span{}
}

// Keys.

// newKey returns the key for the JSON string at buf[start:end].
func (p *RawPatcher) newKey(buf uint8, esc bool, start, end int) key {
	c := p.bytesOf(buf)[start+1 : end-1]
	if esc {
		p.ka = unescape(p.ka[:0], c)
		c = p.ka
	}
	var pre uint64
	for i := 0; i < 8 && i < len(c); i++ {
		pre |= uint64(c[i]) << (56 - 8*uint(i))
	}
	return key{buf: buf, esc: esc, start: start, end: end, pre: pre}
}

// compareKeys compares two keys by their decoded content. Their first 8
// bytes, compared as one integer, decide nearly every comparison; only keys
// that agree on them are compared in full, in compareKeysSlow. This keeps
// compareKeys small enough to inline.
func (p *RawPatcher) compareKeys(a, b key) int {
	if a.pre != b.pre {
		if a.pre < b.pre {
			return -1
		}
		return 1
	}
	return p.compareKeysSlow(a, b)
}

func (p *RawPatcher) compareKeysSlow(a, b key) int {
	ab := p.bytesOf(a.buf)[a.start+1 : a.end-1]
	bb := p.bytesOf(b.buf)[b.start+1 : b.end-1]
	if a.esc {
		p.ka = unescape(p.ka[:0], ab)
		ab = p.ka
	}
	if b.esc {
		p.kb = unescape(p.kb[:0], bb)
		bb = p.kb
	}
	return bytes.Compare(ab, bb)
}

type memberSorter struct {
	p *RawPatcher
	s []member
}

func (s *memberSorter) Len() int           { return len(s.s) }
func (s *memberSorter) Less(i, j int) bool { return s.p.compareKeys(s.s[i].key, s.s[j].key) < 0 }
func (s *memberSorter) Swap(i, j int)      { s.s[i], s.s[j] = s.s[j], s.s[i] }

type changeSorter struct {
	p *RawPatcher
	s []item
}

func (s *changeSorter) Len() int           { return len(s.s) }
func (s *changeSorter) Less(i, j int) bool { return s.p.compareKeys(s.s[i].key, s.s[j].key) < 0 }
func (s *changeSorter) Swap(i, j int)      { s.s[i], s.s[j] = s.s[j], s.s[i] }

// Writing the result.

var null = []byte("null")

func (p *RawPatcher) emit(dst []byte, r ref) []byte {
	switch r.kind {
	case refNull:
		return append(dst, null...)
	case refRaw:
		return append(dst, p.bytesOf(r.buf)[r.start:r.end]...)
	}
	// As outputEntry.result: the object slot, else the array slot, else a
	// non-empty string slot, else the source.
	n := &p.nodes[r.start]
	if n.object {
		return p.emitObject(dst, n)
	}
	if n.array {
		dst = append(dst, '[')
		comma := false
		for i := n.first; i >= 0; i = p.items[i].next {
			if it := p.items[i]; it.slot == slotArray {
				if comma {
					dst = append(dst, ',')
				}
				comma = true
				dst = p.emit(dst, it.val)
			}
		}
		return append(dst, ']')
	}
	mark := len(dst)
	dst = append(dst, '"')
	for i := n.first; i >= 0; i = p.items[i].next {
		if it := p.items[i]; it.slot == slotString {
			dst = append(dst, p.bytesOf(it.val.buf)[it.val.start:it.val.end]...)
		}
	}
	if len(dst) > mark+1 {
		return append(dst, '"')
	}
	return p.emit(dst[:mark], n.src)
}

// emitObject writes an object slot: its source's fields merged with the
// fields set and deleted on top, in key order.
func (p *RawPatcher) emitObject(dst []byte, n *node) []byte {
	var base []member
	if n.src.kind == refRaw {
		if !n.indexed {
			n.members, n.indexed = p.index(n.src.buf, span{n.src.start, n.src.end}, span{}), true
		}
		base = p.members[n.members.start:n.members.end]
	}

	// The changes, sorted by key, keeping the last change to each key.
	mark := len(p.changes)
	for i := n.first; i >= 0; i = p.items[i].next {
		if p.items[i].slot == slotObject {
			p.changes = append(p.changes, p.items[i])
		}
	}
	changes := p.changes[mark:]
	if len(changes) > 1 {
		p.changeSort = changeSorter{p, changes}
		sort.Stable(&p.changeSort)
		p.changeSort = changeSorter{}
		w := 0
		for j := range changes {
			if j+1 < len(changes) && p.compareKeys(changes[j].key, changes[j+1].key) == 0 {
				continue
			}
			changes[w] = changes[j]
			w++
		}
		changes = changes[:w]
	}

	dst = append(dst, '{')
	comma := false
	for len(base) > 0 || len(changes) > 0 {
		c := 1
		switch {
		case len(base) == 0:
		case len(changes) == 0:
			c = -1
		default:
			c = p.compareKeys(base[0].key, changes[0].key)
		}
		var k key
		var v ref
		if c < 0 {
			m := base[0]
			base = base[1:]
			k, v = m.key, ref{kind: refRaw, buf: n.src.buf, start: m.val.start, end: m.val.end}
		} else {
			if c == 0 {
				base = base[1:] // replaced or deleted
			}
			ch := changes[0]
			changes = changes[1:]
			if ch.del {
				continue
			}
			k, v = ch.key, ch.val
		}
		if comma {
			dst = append(dst, ',')
		}
		comma = true
		dst = append(dst, p.bytesOf(k.buf)[k.start:k.end]...)
		dst = append(dst, ':')
		dst = p.emit(dst, v)
	}
	p.changes = p.changes[:mark]
	return append(dst, '}')
}
