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

var errTooLarge = errors.New("mendoza: document or patch larger than 2 GiB")

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
	doc, patch, scratch []byte

	pos int // read offset in patch

	in     []input
	out    []ref
	nodes  []node
	items  []item
	fields []field
	elems  []elem
	kids   []kid
	idx    []int32

	ka, kb, str []byte // decode buffers

	fieldSort fieldSorter
	itemSort  itemSorter
}

var rawPatchers = sync.Pool{New: func() interface{} { return new(RawPatcher) }}

// ApplyRawPatch applies a JSON encoded patch to a JSON encoded document and
// returns the JSON encoded result. See RawPatcher.
func ApplyRawPatch(doc, patch []byte) ([]byte, error) {
	p := rawPatchers.Get().(*RawPatcher)
	defer rawPatchers.Put(p)
	return p.Apply(nil, doc, patch)
}

// Buffers a span can point into.
const (
	bufDoc uint8 = iota
	bufPatch
	bufScratch
	bufConst
)

// constBuf holds the empty key, used by ReturnIntoObjectSameKey at the root.
var constBuf = []byte(`""`)

var emptyKey = key{buf: bufConst, start: 0, end: 2}

// Kinds of ref.
const (
	refNull  uint8 = iota // null, or Blank
	refSpan               // a whole JSON value
	refNode               // a value under construction: nodes[start]
	refElems              // in an array: comma separated elements
	refChars              // in a string: escaped content, without quotes
)

// A ref is an output value, or a piece of one.
type ref struct {
	kind       uint8
	buf        uint8
	start, end int32
}

// A key is a JSON string, quotes included.
type key struct {
	buf        uint8
	esc        bool
	start, end int32
	pre        uint64 // the first 8 bytes of its decoded content, big-endian
}

// A field is an object member of a document or patch value.
type field struct {
	key         key
	start, end  int32 // the value
	kids, nkids int32 // the value's kids, or -1
}

// An elem is an array element in the document.
type elem struct {
	start, end  int32
	kids, nkids int32 // its kids, or -1
}

// A kid is a string or container directly inside a document container that
// was skipped over: skipping stops at those anyway, so recording them is
// nearly free, and indexing the container later jumps from kid to kid
// instead of scanning it again. Numbers and literals are not recorded;
// indexing scans the gaps between kids for them.
type kid struct {
	start, end int32
	esc        bool // strings: has escapes
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
//
// The node's items form a list, each tagged with its slot: the object's are
// fields set or deleted on top of base, the array's are its elements, the
// string's are its pieces.
type node struct {
	src         ref   // refNull once the array or string slot took it
	hasObject   bool  // the object slot is set
	hasArray    bool  // the array slot is set, as a non-nil slice
	base        ref   // the object slot starts from this object, or refNull
	fields      int32 // base's fields in p.fields, or -1
	nfields     int32
	first, last int32
}

type item struct {
	key  key
	slot uint8
	del  bool
	val  ref
	next int32
}

// An input is an entry on the input stack: a value in the document.
type input struct {
	start, end int32
	key        key   // the key it was pushed from; empty for elements and the root
	kids       int32 // in p.kids, or -1 if not recorded
	nkids      int32
	fields     int32 // in p.fields, or -1 until indexed
	nfields    int32
	elems      int32 // in p.elems, or -1 until indexed
	nelems     int32
}

// Apply appends to dst the result of applying patch to doc. dst must not
// overlap doc or patch.
func (p *RawPatcher) Apply(dst, doc, patch []byte) ([]byte, error) {
	if len(doc) > math.MaxInt32 || len(patch) > math.MaxInt32 {
		return dst, errTooLarge
	}
	p.reset(doc, patch)
	defer p.release()

	start := skipWS(doc, 0)
	end := trimEnd(doc, start, len(doc))
	if start == end {
		return dst, ErrInvalidJSON
	}
	switch doc[start] {
	case '{', '[':
		// Don't scan the whole document only to find where it ends: the
		// patch scans what it needs. Check the brackets match.
		if end-start < 2 || doc[end-1] != doc[start]+2 {
			return dst, ErrInvalidJSON
		}
	default:
		if skipValue(doc, start) != end {
			return dst, ErrInvalidJSON
		}
	}
	p.in = append(p.in, input{start: int32(start), end: int32(end), key: emptyKey, kids: -1, fields: -1, elems: -1})
	root := ref{kind: refSpan, buf: bufDoc, start: int32(start), end: int32(end)}
	if doc[start] == 'n' {
		root = ref{} // null: ApplyPatch treats a nil source like Blank.
	}
	p.out = append(p.out, root)

	if err := p.run(); err != nil {
		return dst, err
	}
	if len(p.out) == 0 {
		return dst, ErrInvalidPatch
	}
	return p.emit(dst, p.out[len(p.out)-1])
}

func (p *RawPatcher) reset(doc, patch []byte) {
	p.doc, p.patch = doc, patch
	p.scratch = p.scratch[:0]
	p.pos = 0
	p.in = p.in[:0]
	p.out = p.out[:0]
	p.nodes = p.nodes[:0]
	p.items = p.items[:0]
	p.fields = p.fields[:0]
	p.elems = p.elems[:0]
	p.kids = p.kids[:0]
	p.idx = p.idx[:0]
}

// release drops the caller's slices so a pooled RawPatcher doesn't keep
// them alive.
func (p *RawPatcher) release() {
	p.doc, p.patch = nil, nil
}

func (p *RawPatcher) bytesOf(buf uint8) []byte {
	switch buf {
	case bufDoc:
		return p.doc
	case bufPatch:
		return p.patch
	case bufScratch:
		return p.scratch
	default:
		return constBuf
	}
}

// Reading the patch.

// run executes the patch: a flat JSON array of opcodes and their params,
// read and executed one op at a time.
func (p *RawPatcher) run() error {
	b := p.patch
	i := skipWS(b, 0)
	if i >= len(b) || b[i] != '[' {
		return ErrInvalidJSON
	}
	i = skipWS(b, i+1)
	if i < len(b) && b[i] == ']' {
		p.pos = i + 1
		return p.end()
	}
	p.pos = i
	for {
		code, err := p.readUint()
		if err != nil {
			return err
		}
		if err := p.exec(code); err != nil {
			return err
		}
		i := skipWS(b, p.pos)
		if i >= len(b) {
			return ErrInvalidJSON
		}
		if b[i] == ']' {
			p.pos = i + 1
			return p.end()
		}
		if b[i] != ',' {
			return ErrInvalidJSON
		}
		p.pos = skipWS(b, i+1)
	}
}

func (p *RawPatcher) end() error {
	if skipWS(p.patch, p.pos) != len(p.patch) {
		return ErrInvalidJSON
	}
	return nil
}

// param moves to the next param: past whitespace and one comma.
func (p *RawPatcher) param() error {
	b := p.patch
	i := skipWS(b, p.pos)
	if i >= len(b) || b[i] != ',' {
		return ErrInvalidJSON
	}
	p.pos = skipWS(b, i+1)
	return nil
}

func (p *RawPatcher) readUint() (int, error) {
	b := p.patch
	i := p.pos
	n := 0
	for i < len(b) && '0' <= b[i] && b[i] <= '9' {
		n = n*10 + int(b[i]-'0')
		if n > math.MaxInt32 {
			return 0, ErrInvalidPatch
		}
		i++
	}
	if i == p.pos || (i < len(b) && !scalarEnd[b[i]]) {
		return p.readUintSlow()
	}
	p.pos = i
	return n, nil
}

// readUintSlow accepts integers written as floats, such as 1.0 or 1e2, as
// ReadUintFromValueReader does.
func (p *RawPatcher) readUintSlow() (int, error) {
	e := skipValue(p.patch, p.pos)
	if e < 0 {
		return 0, ErrInvalidJSON
	}
	f, err := strconv.ParseFloat(string(p.patch[p.pos:e]), 64)
	if err != nil || f < 0 || f > math.MaxInt32 || f != math.Trunc(f) {
		return 0, ErrInvalidPatch
	}
	p.pos = e
	return int(f), nil
}

func (p *RawPatcher) paramUint() (int, error) {
	if err := p.param(); err != nil {
		return 0, err
	}
	return p.readUint()
}

func (p *RawPatcher) paramString() (key, error) {
	if err := p.param(); err != nil {
		return key{}, err
	}
	b := p.patch
	if p.pos >= len(b) || b[p.pos] != '"' {
		return key{}, ErrInvalidPatch
	}
	e, esc := skipString(b, p.pos)
	if e < 0 {
		return key{}, ErrInvalidJSON
	}
	k := p.newKey(bufPatch, esc, p.pos, e)
	p.pos = e
	return k, nil
}

func (p *RawPatcher) paramValue() (ref, error) {
	if err := p.param(); err != nil {
		return ref{}, err
	}
	e := skipValue(p.patch, p.pos)
	if e < 0 {
		return ref{}, ErrInvalidJSON
	}
	r := ref{kind: refSpan, buf: bufPatch, start: int32(p.pos), end: int32(e)}
	if p.patch[p.pos] == 'n' {
		r = ref{} // null behaves like Blank, as a nil source does in ApplyPatch.
	}
	p.pos = e
	return r, nil
}

// Executing ops.

func (p *RawPatcher) exec(code int) error {
	switch code {
	case int(codeValue):
		v, err := p.paramValue()
		if err != nil {
			return err
		}
		p.out = append(p.out, v)
		return nil
	case int(codeCopy):
		return p.opCopy()
	case int(codeBlank):
		p.out = append(p.out, ref{})
		return nil
	case int(codeReturnIntoArray):
		return p.opReturnIntoArray()
	case int(codeReturnIntoObject):
		k, err := p.paramString()
		if err != nil {
			return err
		}
		return p.opReturnIntoObject(k)
	case int(codeReturnIntoObjectSameKey):
		return p.opReturnIntoObjectSameKey()
	case int(codePushField):
		n, err := p.paramUint()
		if err != nil {
			return err
		}
		return p.opPushField(n)
	case int(codePushElement):
		n, err := p.paramUint()
		if err != nil {
			return err
		}
		return p.opPushElement(n)
	case int(codePushParent):
		n, err := p.paramUint()
		if err != nil {
			return err
		}
		return p.opPushParent(n)
	case int(codePop):
		return p.opPop()
	case int(codePushFieldCopy):
		n, err := p.paramUint()
		if err != nil {
			return err
		}
		if err := p.opPushField(n); err != nil {
			return err
		}
		return p.opCopy()
	case int(codePushFieldBlank):
		n, err := p.paramUint()
		if err != nil {
			return err
		}
		if err := p.opPushField(n); err != nil {
			return err
		}
		p.out = append(p.out, ref{})
		return nil
	case int(codePushElementCopy):
		n, err := p.paramUint()
		if err != nil {
			return err
		}
		if err := p.opPushElement(n); err != nil {
			return err
		}
		return p.opCopy()
	case int(codePushElementBlank):
		n, err := p.paramUint()
		if err != nil {
			return err
		}
		if err := p.opPushElement(n); err != nil {
			return err
		}
		p.out = append(p.out, ref{})
		return nil
	case int(codeReturnIntoObjectPop):
		k, err := p.paramString()
		if err != nil {
			return err
		}
		if err := p.opReturnIntoObject(k); err != nil {
			return err
		}
		return p.opPop()
	case int(codeReturnIntoObjectSameKeyPop):
		if err := p.opReturnIntoObjectSameKey(); err != nil {
			return err
		}
		return p.opPop()
	case int(codeReturnIntoArrayPop):
		if err := p.opReturnIntoArray(); err != nil {
			return err
		}
		return p.opPop()
	case int(codeObjectSetFieldValue):
		v, err := p.paramValue()
		if err != nil {
			return err
		}
		k, err := p.paramString()
		if err != nil {
			return err
		}
		p.out = append(p.out, v)
		return p.opReturnIntoObject(k)
	case int(codeObjectCopyField):
		n, err := p.paramUint()
		if err != nil {
			return err
		}
		if err := p.opPushField(n); err != nil {
			return err
		}
		if err := p.opCopy(); err != nil {
			return err
		}
		if err := p.opReturnIntoObjectSameKey(); err != nil {
			return err
		}
		return p.opPop()
	case int(codeObjectDeleteField):
		n, err := p.paramUint()
		if err != nil {
			return err
		}
		return p.opObjectDeleteField(n)
	case int(codeArrayAppendValue):
		v, err := p.paramValue()
		if err != nil {
			return err
		}
		return p.opArrayAppend(v)
	case int(codeArrayAppendSlice):
		l, err := p.paramUint()
		if err != nil {
			return err
		}
		r, err := p.paramUint()
		if err != nil {
			return err
		}
		return p.opArrayAppendSlice(l, r)
	case int(codeStringAppendString):
		k, err := p.paramString()
		if err != nil {
			return err
		}
		return p.appendChars(ref{kind: refChars, buf: bufPatch, start: k.start + 1, end: k.end - 1})
	case int(codeStringAppendSlice):
		l, err := p.paramUint()
		if err != nil {
			return err
		}
		r, err := p.paramUint()
		if err != nil {
			return err
		}
		return p.opStringAppendSlice(l, r)
	default:
		return ErrInvalidPatch
	}
}

func (p *RawPatcher) opCopy() error {
	if len(p.in) == 0 {
		return ErrInvalidPatch
	}
	in := &p.in[len(p.in)-1]
	r := ref{kind: refSpan, buf: bufDoc, start: in.start, end: in.end}
	if p.doc[in.start] == 'n' {
		r = ref{}
	}
	p.out = append(p.out, r)
	return nil
}

func (p *RawPatcher) opPushField(n int) error {
	if len(p.in) == 0 {
		return ErrInvalidPatch
	}
	f, err := p.inputField(n)
	if err != nil {
		return err
	}
	p.in = append(p.in, input{start: f.start, end: f.end, key: f.key, kids: f.kids, nkids: f.nkids, fields: -1, elems: -1})
	return nil
}

func (p *RawPatcher) opPushElement(n int) error {
	if len(p.in) == 0 {
		return ErrInvalidPatch
	}
	first, count, err := p.inputElems()
	if err != nil {
		return err
	}
	if n >= int(count) {
		return ErrInvalidPatch
	}
	e := p.elems[int(first)+n]
	p.in = append(p.in, input{start: e.start, end: e.end, key: emptyKey, kids: e.kids, nkids: e.nkids, fields: -1, elems: -1})
	return nil
}

func (p *RawPatcher) opPushParent(n int) error {
	i := len(p.in) - 2 - n
	if i < 0 {
		return ErrInvalidPatch
	}
	p.in = append(p.in, p.in[i])
	return nil
}

func (p *RawPatcher) opPop() error {
	if len(p.in) == 0 {
		return ErrInvalidPatch
	}
	p.in = p.in[:len(p.in)-1]
	return nil
}

// pop removes the output value, which must not be the last one.
func (p *RawPatcher) pop() (ref, error) {
	if len(p.out) < 2 {
		return ref{}, ErrInvalidPatch
	}
	r := p.out[len(p.out)-1]
	p.out = p.out[:len(p.out)-1]
	return r, nil
}

func (p *RawPatcher) opReturnIntoObject(k key) error {
	v, err := p.pop()
	if err != nil {
		return err
	}
	n, err := p.outputObject()
	if err != nil {
		return err
	}
	p.addItem(n, item{key: k, slot: slotObject, val: v})
	return nil
}

func (p *RawPatcher) opReturnIntoObjectSameKey() error {
	if len(p.in) == 0 {
		return ErrInvalidPatch
	}
	return p.opReturnIntoObject(p.in[len(p.in)-1].key)
}

func (p *RawPatcher) opReturnIntoArray() error {
	v, err := p.pop()
	if err != nil {
		return err
	}
	return p.opArrayAppend(v)
}

func (p *RawPatcher) opArrayAppend(v ref) error {
	n, err := p.outputArray()
	if err != nil {
		return err
	}
	p.nodes[n].hasArray = true
	p.addItem(n, item{slot: slotArray, val: v})
	return nil
}

func (p *RawPatcher) opObjectDeleteField(n int) error {
	if len(p.in) == 0 {
		return ErrInvalidPatch
	}
	f, err := p.inputField(n)
	if err != nil {
		return err
	}
	o, err := p.outputObject()
	if err != nil {
		return err
	}
	p.addItem(o, item{key: f.key, slot: slotObject, del: true})
	return nil
}

func (p *RawPatcher) opArrayAppendSlice(l, r int) error {
	if len(p.in) == 0 {
		return ErrInvalidPatch
	}
	first, count, err := p.inputElems()
	if err != nil {
		return err
	}
	if l > r || r > int(count) {
		return ErrInvalidPatch
	}
	n, err := p.outputArray()
	if err != nil {
		return err
	}
	if l == r {
		return nil
	}
	start := p.elems[int(first)+l].start
	end := p.elems[int(first)+r-1].end
	p.nodes[n].hasArray = true
	p.addItem(n, item{slot: slotArray, val: ref{kind: refElems, buf: bufDoc, start: start, end: end}})
	return nil
}

func (p *RawPatcher) opStringAppendSlice(l, r int) error {
	if len(p.in) == 0 {
		return ErrInvalidPatch
	}
	in := &p.in[len(p.in)-1]
	if p.doc[in.start] != '"' {
		return ErrInvalidPatch
	}
	start, end := int(in.start)+1, int(in.end)-1
	raw := p.doc[start:end]
	if bytes.IndexByte(raw, '\\') < 0 {
		// Unescaped content is its own decoding, so slice it in place.
		if l > r || r > len(raw) {
			return ErrInvalidPatch
		}
		return p.appendChars(ref{kind: refChars, buf: bufDoc, start: int32(start + l), end: int32(start + r)})
	}
	// Offsets are into the decoded string: decode, slice, escape again.
	p.str = unescape(p.str[:0], raw)
	if l > r || r > len(p.str) {
		return ErrInvalidPatch
	}
	s := len(p.scratch)
	p.scratch = escape(p.scratch, p.str[l:r])
	if len(p.scratch) > math.MaxInt32 {
		return errTooLarge
	}
	return p.appendChars(ref{kind: refChars, buf: bufScratch, start: int32(s), end: int32(len(p.scratch))})
}

func (p *RawPatcher) appendChars(c ref) error {
	n, err := p.outputString()
	if err != nil {
		return err
	}
	if c.end > c.start {
		p.addItem(n, item{slot: slotString, val: c})
	}
	return nil
}

// outputNode makes the output value a node, if it is not one already, and
// returns its index.
func (p *RawPatcher) outputNode() int32 {
	t := &p.out[len(p.out)-1]
	if t.kind == refNode {
		return t.start
	}
	idx := int32(len(p.nodes))
	p.nodes = append(p.nodes, node{src: *t, fields: -1, first: -1, last: -1})
	*t = ref{kind: refNode, start: idx}
	return idx
}

// srcByte returns the first byte of the node's source, or 0 if it has none.
func (p *RawPatcher) srcByte(n *node) byte {
	if n.src.kind != refSpan {
		return 0
	}
	return p.bytesOf(n.src.buf)[n.src.start]
}

// outputObject sets the output value's object slot, as
// patcher.outputObject: from the source, which must then be an object, or
// empty. The source stays.
func (p *RawPatcher) outputObject() (int32, error) {
	idx := p.outputNode()
	n := &p.nodes[idx]
	if n.hasObject {
		return idx, nil
	}
	switch p.srcByte(n) {
	case 0:
	case '{':
		n.base = n.src
		// The source is often the input value, already indexed.
		if len(p.in) > 0 && n.src.buf == bufDoc {
			if in := &p.in[len(p.in)-1]; in.start == n.src.start && in.fields >= 0 {
				n.fields, n.nfields = in.fields, in.nfields
			}
		}
	default:
		return 0, ErrInvalidPatch
	}
	n.hasObject = true
	return idx, nil
}

// outputArray prepares the array slot, as patcher.outputArray: a source,
// which must be an array, moves into it.
func (p *RawPatcher) outputArray() (int32, error) {
	idx := p.outputNode()
	n := &p.nodes[idx]
	switch p.srcByte(n) {
	case 0:
		return idx, nil
	case '[':
	default:
		return 0, ErrInvalidPatch
	}
	src := n.src
	n.src = ref{}
	n.hasArray = true
	b := p.bytesOf(src.buf)
	s := skipWS(b, int(src.start)+1)
	if e := trimEnd(b, s, int(src.end)-1); e > s {
		p.addItem(idx, item{slot: slotArray, val: ref{kind: refElems, buf: src.buf, start: int32(s), end: int32(e)}})
	}
	return idx, nil
}

// outputString prepares the string slot, as patcher.outputString: a
// source, which must be a string, moves into it.
func (p *RawPatcher) outputString() (int32, error) {
	idx := p.outputNode()
	n := &p.nodes[idx]
	switch p.srcByte(n) {
	case 0:
		return idx, nil
	case '"':
	default:
		return 0, ErrInvalidPatch
	}
	src := n.src
	n.src = ref{}
	if src.end-src.start > 2 {
		p.addItem(idx, item{slot: slotString, val: ref{kind: refChars, buf: src.buf, start: src.start + 1, end: src.end - 1}})
	}
	return idx, nil
}

func (p *RawPatcher) addItem(n int32, it item) {
	it.next = -1
	i := int32(len(p.items))
	p.items = append(p.items, it)
	nd := &p.nodes[n]
	if nd.last < 0 {
		nd.first = i
	} else {
		p.items[nd.last].next = i
	}
	nd.last = i
}

// Indexing the document.

// inputField returns field n, in key order, of the input value.
func (p *RawPatcher) inputField(n int) (field, error) {
	in := &p.in[len(p.in)-1]
	if in.fields < 0 {
		if p.doc[in.start] != '{' {
			return field{}, ErrInvalidPatch
		}
		first, count, err := p.indexObject(bufDoc, in.start, in.end, in.kids, in.nkids)
		if err != nil {
			return field{}, err
		}
		in = &p.in[len(p.in)-1]
		in.fields, in.nfields = first, count
	}
	if n >= int(in.nfields) {
		return field{}, ErrInvalidPatch
	}
	return p.fields[int(in.fields)+n], nil
}

// inputElems returns the elements of the input value, indexing it if needed.
func (p *RawPatcher) inputElems() (int32, int32, error) {
	in := &p.in[len(p.in)-1]
	if in.elems >= 0 {
		return in.elems, in.nelems, nil
	}
	b := p.doc
	if b[in.start] != '[' {
		return 0, 0, ErrInvalidPatch
	}
	first := int32(len(p.elems))
	i := skipWS(b, int(in.start)+1)
	end := int(in.end)
	if i < end && b[i] == ']' {
		in.elems, in.nelems = first, 0
		return first, 0, nil
	}
	c := cursor{k: in.kids, end: in.kids + in.nkids}
	for {
		e, _, kids, nkids := p.child(b, i, &c, true)
		if e < 0 || e > end {
			return 0, 0, ErrInvalidJSON
		}
		p.elems = append(p.elems, elem{start: int32(i), end: int32(e), kids: kids, nkids: nkids})
		i = skipWS(b, e)
		if i >= end {
			return 0, 0, ErrInvalidJSON
		}
		if b[i] == ']' {
			break
		}
		if b[i] != ',' {
			return 0, 0, ErrInvalidJSON
		}
		i = skipWS(b, i+1)
	}
	in.elems, in.nelems = first, int32(len(p.elems))-first
	return in.elems, in.nelems, nil
}

// indexObject appends the fields of the object at buf[start:end] to
// p.fields, sorted by key with the last of any duplicates kept, which is the
// order and content ApplyPatch sees in a decoded map.
func (p *RawPatcher) indexObject(buf uint8, start, end, kids, nkids int32) (int32, int32, error) {
	b := p.bytesOf(buf)
	first := len(p.fields)
	i := skipWS(b, int(start)+1)
	e := int(end)
	if i < e && b[i] == '}' {
		return int32(first), 0, nil
	}
	c := cursor{k: kids, end: kids + nkids}
	record := buf == bufDoc
	sorted := true
	for {
		if i >= e || b[i] != '"' {
			return 0, 0, ErrInvalidJSON
		}
		ke, esc, _, _ := p.child(b, i, &c, false)
		if ke < 0 {
			return 0, 0, ErrInvalidJSON
		}
		k := p.newKey(buf, esc, i, ke)
		i = skipWS(b, ke)
		if i >= e || b[i] != ':' {
			return 0, 0, ErrInvalidJSON
		}
		i = skipWS(b, i+1)
		ve, _, vk, vn := p.child(b, i, &c, record)
		if ve < 0 || ve > e {
			return 0, 0, ErrInvalidJSON
		}
		if sorted && len(p.fields) > first && p.compareKeys(p.fields[len(p.fields)-1].key, k) >= 0 {
			sorted = false
		}
		p.fields = append(p.fields, field{key: k, start: int32(i), end: int32(ve), kids: vk, nkids: vn})
		i = skipWS(b, ve)
		if i >= e {
			return 0, 0, ErrInvalidJSON
		}
		if b[i] == '}' {
			break
		}
		if b[i] != ',' {
			return 0, 0, ErrInvalidJSON
		}
		i = skipWS(b, i+1)
	}
	if !sorted {
		fs := p.fields[first:]
		p.sortFields(fs)
		w := 0
		for j := range fs {
			if j+1 < len(fs) && p.compareKeys(fs[j].key, fs[j+1].key) == 0 {
				continue // a later duplicate wins
			}
			fs[w] = fs[j]
			w++
		}
		p.fields = p.fields[:first+w]
	}
	return int32(first), int32(len(p.fields) - first), nil
}

// A cursor walks the kids recorded for a container, in order.
type cursor struct{ k, end int32 }

// child returns the end of the value at b[i], whether it is a string with
// escapes, and, if record is set and it is a container, its kids. A kid
// recorded at i gives the end without scanning.
func (p *RawPatcher) child(b []byte, i int, c *cursor, record bool) (int, bool, int32, int32) {
	if c.k < c.end {
		if k := p.kids[c.k]; int(k.start) == i {
			c.k++
			return int(k.end), k.esc, -1, 0
		}
	}
	if i < len(b) {
		switch b[i] {
		case '"':
			e, esc := skipString(b, i)
			return e, esc, -1, 0
		case '{', '[':
			if record {
				return p.skipRecord(b, i)
			}
		}
	}
	return skipValue(b, i), false, -1, 0
}

// skipRecord is skipContainer, recording the container's kids.
func (p *RawPatcher) skipRecord(b []byte, i int) (int, bool, int32, int32) {
	first := int32(len(p.kids))
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
				return -1, false, -1, 0
			}
			if depth == 1 {
				p.kids = append(p.kids, kid{start: int32(i), end: int32(e), esc: esc})
			}
			i = e
			continue
		case '{', '[':
			depth++
			if depth == 2 {
				open = i
			}
		default: // '}', ']'
			depth--
			if depth == 1 {
				p.kids = append(p.kids, kid{start: int32(open), end: int32(i + 1)})
			} else if depth == 0 {
				return i + 1, false, first, int32(len(p.kids)) - first
			}
		}
		i++
	}
	return -1, false, -1, 0
}

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
	return key{buf: buf, esc: esc, start: int32(start), end: int32(end), pre: pre}
}

// compareKeys compares two keys by their decoded content. Their first 8
// bytes, compared as one integer, decide nearly every comparison; only keys
// that agree on them are compared in full, in compareKeysSlow. This keeps
// compareKeys small enough to inline into the sorts.
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

// smallSort is the size up to which sortFields and sortItems use insertion
// sort, calling compareKeys directly rather than through sort.Interface.
// Most objects are smaller.
const smallSort = 24

// sortFields sorts fields stably by key.
func (p *RawPatcher) sortFields(fs []field) {
	if len(fs) > smallSort {
		p.fieldSort.p, p.fieldSort.s = p, fs
		sort.Stable(&p.fieldSort)
		p.fieldSort.p, p.fieldSort.s = nil, nil
		return
	}
	for i := 1; i < len(fs); i++ {
		f := fs[i]
		j := i
		for ; j > 0 && p.compareKeys(f.key, fs[j-1].key) < 0; j-- {
			fs[j] = fs[j-1]
		}
		fs[j] = f
	}
}

// sortItems sorts item indices stably by their items' keys.
func (p *RawPatcher) sortItems(ch []int32) {
	if len(ch) > smallSort {
		p.itemSort.p, p.itemSort.s = p, ch
		sort.Stable(&p.itemSort)
		p.itemSort.p, p.itemSort.s = nil, nil
		return
	}
	for i := 1; i < len(ch); i++ {
		c := ch[i]
		k := p.items[c].key
		j := i
		for ; j > 0 && p.compareKeys(k, p.items[ch[j-1]].key) < 0; j-- {
			ch[j] = ch[j-1]
		}
		ch[j] = c
	}
}

type fieldSorter struct {
	p *RawPatcher
	s []field
}

func (s *fieldSorter) Len() int           { return len(s.s) }
func (s *fieldSorter) Less(i, j int) bool { return s.p.compareKeys(s.s[i].key, s.s[j].key) < 0 }
func (s *fieldSorter) Swap(i, j int)      { s.s[i], s.s[j] = s.s[j], s.s[i] }

type itemSorter struct {
	p *RawPatcher
	s []int32
}

func (s *itemSorter) Len() int { return len(s.s) }
func (s *itemSorter) Less(i, j int) bool {
	return s.p.compareKeys(s.p.items[s.s[i]].key, s.p.items[s.s[j]].key) < 0
}
func (s *itemSorter) Swap(i, j int) { s.s[i], s.s[j] = s.s[j], s.s[i] }

// Writing the result.

var null = []byte("null")

func (p *RawPatcher) emit(dst []byte, r ref) ([]byte, error) {
	switch r.kind {
	case refNull:
		return append(dst, null...), nil
	case refSpan, refElems:
		return append(dst, p.bytesOf(r.buf)[r.start:r.end]...), nil
	}
	// As outputEntry.result: the object slot, else the array slot, else a
	// non-empty string slot, else the source.
	n := p.nodes[r.start]
	if n.hasObject {
		return p.emitObject(dst, r.start)
	}
	if n.hasArray {
		dst = append(dst, '[')
		first := true
		for i := n.first; i >= 0; i = p.items[i].next {
			if p.items[i].slot != slotArray {
				continue
			}
			if !first {
				dst = append(dst, ',')
			}
			first = false
			var err error
			if dst, err = p.emit(dst, p.items[i].val); err != nil {
				return dst, err
			}
		}
		return append(dst, ']'), nil
	}
	mark := len(dst)
	dst = append(dst, '"')
	for i := n.first; i >= 0; i = p.items[i].next {
		if c := p.items[i].val; p.items[i].slot == slotString {
			dst = append(dst, p.bytesOf(c.buf)[c.start:c.end]...)
		}
	}
	if len(dst) > mark+1 {
		return append(dst, '"'), nil
	}
	return p.emit(dst[:mark], n.src)
}

// emitObject writes an object node: the base object's fields merged with
// the fields set and deleted on top, all in key order.
func (p *RawPatcher) emitObject(dst []byte, ni int32) ([]byte, error) {
	n := &p.nodes[ni]
	var bf, bn int32
	if n.base.kind == refSpan {
		if n.fields < 0 {
			first, count, err := p.indexObject(n.base.buf, n.base.start, n.base.end, -1, 0)
			if err != nil {
				return dst, err
			}
			n = &p.nodes[ni]
			n.fields, n.nfields = first, count
		}
		bf, bn = n.fields, n.nfields
	}
	base := p.bytesOf(n.base.buf)

	// Collect the changes, sort them stably by key, and keep the last
	// change to each key.
	mark := len(p.idx)
	for i := n.first; i >= 0; i = p.items[i].next {
		if p.items[i].slot == slotObject {
			p.idx = append(p.idx, i)
		}
	}
	if len(p.idx)-mark > 1 {
		ch := p.idx[mark:]
		p.sortItems(ch)
		w := 0
		for j := range ch {
			if j+1 < len(ch) && p.compareKeys(p.items[ch[j]].key, p.items[ch[j+1]].key) == 0 {
				continue
			}
			ch[w] = ch[j]
			w++
		}
		p.idx = p.idx[:mark+w]
	}
	end := len(p.idx)

	dst = append(dst, '{')
	wrote := false
	bi, ci := int32(0), mark
	for bi < bn || ci < end {
		c := 1
		switch {
		case bi >= bn:
		case ci >= end:
			c = -1
		default:
			c = p.compareKeys(p.fields[bf+bi].key, p.items[p.idx[ci]].key)
		}
		if wrote {
			dst = append(dst, ',')
		}
		if c < 0 {
			f := p.fields[bf+bi]
			bi++
			dst = append(dst, base[f.key.start:f.key.end]...)
			dst = append(dst, ':')
			dst = append(dst, base[f.start:f.end]...)
			wrote = true
			continue
		}
		if c == 0 {
			bi++
		}
		it := p.items[p.idx[ci]]
		ci++
		if it.del {
			if wrote {
				dst = dst[:len(dst)-1]
			}
			continue
		}
		dst = append(dst, p.bytesOf(it.key.buf)[it.key.start:it.key.end]...)
		dst = append(dst, ':')
		var err error
		if dst, err = p.emit(dst, it.val); err != nil {
			return dst, err
		}
		wrote = true
	}
	p.idx = p.idx[:mark]
	return append(dst, '}'), nil
}
