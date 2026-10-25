package mendoza_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"testing"
	"unicode/utf8"

	"github.com/sanity-io/mendoza"
	"github.com/stretchr/testify/require"
)

// checkRaw applies the patch from left to right with RawPatcher, to left in
// every encoding, and checks the result equals right, and ApplyPatch's.
func checkRaw(t testing.TB, left, right interface{}, rng *rand.Rand) {
	patch, err := mendoza.CreatePatch(left, right)
	require.NoError(t, err)
	patchJSON, err := json.Marshal(patch)
	require.NoError(t, err)

	want := normalize(t, mendoza.ApplyPatch(left, patch))
	require.Equal(t, normalize(t, right), want, "ApplyPatch itself")

	var p mendoza.RawPatcher
	for _, style := range styles {
		doc := encode(nil, left, style, rng)
		got, err := p.Apply(nil, doc, patchJSON)
		require.NoError(t, err, "style %s\ndoc:   %s\npatch: %s", style.name, doc, patchJSON)
		require.True(t, json.Valid(got), "style %s: invalid JSON %s", style.name, got)
		require.Equal(t, want, normalize(t, decode(t, got)),
			"style %s\ndoc:   %s\npatch: %s\ngot:   %s", style.name, doc, patchJSON, got)
	}
}

func decode(t testing.TB, b []byte) interface{} {
	var v interface{}
	require.NoError(t, json.Unmarshal(b, &v), "%s", b)
	return v
}

// normalize round trips v through encoding/json, so ints and float64s
// compare equal.
func normalize(t testing.TB, v interface{}) interface{} {
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return decode(t, b)
}

func TestRawPatchDocuments(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for i, pair := range Documents {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			var left, right interface{}
			require.NoError(t, json.Unmarshal([]byte(pair.Left), &left))
			require.NoError(t, json.Unmarshal([]byte(pair.Right), &right))
			checkRaw(t, left, right, rng)
		})
	}
}

func TestRawPatchRandom(t *testing.T) {
	n := 3000
	if testing.Short() {
		n = 300
	}
	for seed := 0; seed < n; seed++ {
		rng := rand.New(rand.NewSource(int64(seed)))
		left := genValue(rng, 4)
		right := mutate(rng, left, 4)
		t.Run(strconv.Itoa(seed), func(t *testing.T) {
			checkRaw(t, left, right, rng)
		})
	}
}

func FuzzRawPatch(f *testing.F) {
	for i := int64(0); i < 20; i++ {
		f.Add(i)
	}
	f.Fuzz(func(t *testing.T, seed int64) {
		rng := rand.New(rand.NewSource(seed))
		left := genValue(rng, 5)
		checkRaw(t, left, mutate(rng, left, 5), rng)
	})
}

// FuzzRawPatchBytes feeds arbitrary bytes: RawPatcher must not panic.
func FuzzRawPatchBytes(f *testing.F) {
	f.Add([]byte(`{"a":"abc","b":[1,2,3]}`), []byte(`[19,0,10,1,21,0,2,15]`))
	f.Add([]byte(`"abc\u00e9d"`), []byte(`[2,23,0,3,22,"x"]`))
	f.Add([]byte(`{"a":{"b":[1,{"c":2}]}}`), []byte(`[10,0,11,0,12,1,17,3,"d",16,15,15]`))
	f.Fuzz(func(t *testing.T, doc, patch []byte) {
		var p mendoza.RawPatcher
		out, err := p.Apply(nil, doc, patch)
		// Values are copied, not validated, so only valid input promises
		// valid output.
		if err == nil && json.Valid(doc) && json.Valid(patch) && !json.Valid(out) {
			t.Fatalf("valid doc %s and patch %s gave invalid %s", doc, patch, out)
		}
	})
}

func TestRawPatchErrors(t *testing.T) {
	var p mendoza.RawPatcher
	for _, tc := range []struct{ doc, patch string }{
		{`{"a":1}`, ``},
		{`{"a":1}`, `[`},
		{`{"a":1}`, `[6,5]`},  // no field 5
		{`{"a":1}`, `[7,0]`},  // not an array
		{`{"a":1}`, `[9,9]`},  // pop past the root
		{`{"a":1}`, `[99]`},   // unknown op
		{`{"a":1}`, `[6,-1]`}, // negative index
		{`{"a":1`, `[]`},
		{`[1,2]`, `[21,1,3]`},
		{`"ab"`, `[2,23,0,3]`},
	} {
		_, err := p.Apply(nil, []byte(tc.doc), []byte(tc.patch))
		require.Error(t, err, "doc %s patch %s", tc.doc, tc.patch)
	}
}

func TestRawPatchKeepsNumbers(t *testing.T) {
	doc := []byte(`{"big": 12345678901234567891, "f": 1.50, "s": "x"}`)
	// Set s to "y", copying everything else.
	got, err := mendoza.ApplyRawPatch(doc, []byte(`[17,"y","s"]`))
	require.NoError(t, err)
	require.Equal(t, `{"big":12345678901234567891,"f":1.50,"s":"y"}`, string(got))
}

func TestRawPatchAllocs(t *testing.T) {
	left := genDoc(50)
	right := editDoc(left)
	patch, _ := mendoza.CreatePatch(left, right)
	patchJSON, _ := json.Marshal(patch)
	doc, _ := json.Marshal(left)
	var p mendoza.RawPatcher
	dst, err := p.Apply(nil, doc, patchJSON)
	require.NoError(t, err)
	allocs := testing.AllocsPerRun(100, func() {
		dst, _ = p.Apply(dst[:0], doc, patchJSON)
	})
	require.Zero(t, allocs)
}

// Generating documents.

var keyAlphabet = []string{"a", "b", "c", "_key", "_type", "title", "aa", "ab", "é", "\n", "\"q", "k/", "Z", "😀"}

var runeAlphabet = []rune("abcdefghij ABC\n\t\"\\/é😀\u2028\x01<>&")

func genString(rng *rand.Rand) string {
	n := rng.Intn(12)
	rs := make([]rune, n)
	for i := range rs {
		rs[i] = runeAlphabet[rng.Intn(len(runeAlphabet))]
	}
	return string(rs)
}

func genValue(rng *rand.Rand, depth int) interface{} {
	k := rng.Intn(10)
	if depth <= 0 {
		k = rng.Intn(6)
	}
	switch k {
	case 0:
		return nil
	case 1:
		return rng.Intn(2) == 0
	case 2:
		return float64(rng.Intn(2000) - 1000)
	case 3:
		return rng.NormFloat64() * 1e3
	case 4, 5:
		return genString(rng)
	case 6, 7:
		n := rng.Intn(6)
		arr := make([]interface{}, n)
		for i := range arr {
			arr[i] = genValue(rng, depth-1)
		}
		return arr
	default:
		n := rng.Intn(7)
		obj := make(map[string]interface{}, n)
		for i := 0; i < n; i++ {
			obj[keyAlphabet[rng.Intn(len(keyAlphabet))]] = genValue(rng, depth-1)
		}
		return obj
	}
}

// mutate returns a copy of v with random edits.
func mutate(rng *rand.Rand, v interface{}, depth int) interface{} {
	if rng.Intn(8) == 0 {
		return genValue(rng, depth)
	}
	switch v := v.(type) {
	case map[string]interface{}:
		obj := make(map[string]interface{}, len(v))
		for k, val := range v {
			switch rng.Intn(6) {
			case 0: // delete
			case 1: // rename
				obj[keyAlphabet[rng.Intn(len(keyAlphabet))]] = val
			case 2, 3:
				obj[k] = mutate(rng, val, depth-1)
			default:
				obj[k] = val
			}
		}
		for i := rng.Intn(3); i > 0; i-- {
			obj[keyAlphabet[rng.Intn(len(keyAlphabet))]] = genValue(rng, depth-1)
		}
		return obj
	case []interface{}:
		arr := make([]interface{}, 0, len(v)+2)
		for _, val := range v {
			switch rng.Intn(6) {
			case 0:
			case 1:
				arr = append(arr, genValue(rng, depth-1), val)
			case 2:
				arr = append(arr, mutate(rng, val, depth-1))
			default:
				arr = append(arr, val)
			}
		}
		if rng.Intn(3) == 0 {
			rng.Shuffle(len(arr), func(i, j int) { arr[i], arr[j] = arr[j], arr[i] })
		}
		return arr
	case string:
		rs := []rune(v)
		if len(rs) > 0 && rng.Intn(2) == 0 {
			i := rng.Intn(len(rs))
			j := i + rng.Intn(len(rs)-i+1)
			rs = append(rs[:i:i], rs[j:]...)
		}
		i := rng.Intn(len(rs) + 1)
		ins := []rune(genString(rng))
		return string(append(append(append([]rune{}, rs[:i]...), ins...), rs[i:]...))
	default:
		return genValue(rng, depth)
	}
}

// Encoding documents in different styles, to check RawPatcher does not
// depend on how the document was written.

type style struct {
	name   string
	order  func(keys []string, rng *rand.Rand) // sorts keys in place
	space  bool                                // spaces after : and , as Postgres writes jsonb
	ascii  bool                                // escape all non-ASCII as \u
	random bool                                // escape random ASCII as \u00XX, and / as \/
	indent bool
}

var styles = []style{
	{name: "go", order: func(k []string, _ *rand.Rand) { sort.Strings(k) }},
	{name: "jsonb", order: jsonbOrder, space: true},
	{name: "shuffled", order: shuffle, ascii: true, random: true, indent: true},
}

// jsonbOrder is the order Postgres keeps jsonb keys in: by length, then bytes.
func jsonbOrder(k []string, _ *rand.Rand) {
	sort.Slice(k, func(i, j int) bool {
		if len(k[i]) != len(k[j]) {
			return len(k[i]) < len(k[j])
		}
		return k[i] < k[j]
	})
}

func shuffle(k []string, rng *rand.Rand) {
	rng.Shuffle(len(k), func(i, j int) { k[i], k[j] = k[j], k[i] })
}

func encode(dst []byte, v interface{}, s style, rng *rand.Rand) []byte {
	ws := func() {
		if s.indent && rng.Intn(2) == 0 {
			dst = append(dst, "\n\t "[rng.Intn(3)])
		}
	}
	switch v := v.(type) {
	case nil:
		return append(dst, "null"...)
	case bool:
		return strconv.AppendBool(dst, v)
	case float64:
		b, _ := json.Marshal(v)
		return append(dst, b...)
	case string:
		return encodeString(dst, v, s, rng)
	case []interface{}:
		dst = append(dst, '[')
		for i, e := range v {
			if i > 0 {
				dst = append(dst, ',')
				if s.space {
					dst = append(dst, ' ')
				}
			}
			ws()
			dst = encode(dst, e, s, rng)
			ws()
		}
		return append(dst, ']')
	case map[string]interface{}:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		s.order(keys, rng)
		dst = append(dst, '{')
		for i, k := range keys {
			if i > 0 {
				dst = append(dst, ',')
				if s.space {
					dst = append(dst, ' ')
				}
			}
			ws()
			dst = encodeString(dst, k, s, rng)
			ws()
			dst = append(dst, ':')
			if s.space {
				dst = append(dst, ' ')
			}
			ws()
			dst = encode(dst, v[k], s, rng)
			ws()
		}
		return append(dst, '}')
	default:
		panic(fmt.Sprintf("unexpected %T", v))
	}
}

func encodeString(dst []byte, str string, s style, rng *rand.Rand) []byte {
	if !s.ascii && !s.random {
		b, _ := json.Marshal(str)
		return append(dst, b...)
	}
	dst = append(dst, '"')
	for _, r := range str {
		switch {
		case r == '"' || r == '\\':
			dst = append(dst, '\\', byte(r))
		case r == '/' && s.random && rng.Intn(2) == 0:
			dst = append(dst, '\\', '/')
		case r < 0x20 || r >= 0x80 && s.ascii || s.random && rng.Intn(4) == 0:
			if r > 0xffff {
				r1, r2 := surrogates(r)
				dst = append(dst, fmt.Sprintf(`\u%04x\u%04X`, r1, r2)...)
			} else {
				dst = append(dst, fmt.Sprintf(`\u%04x`, r)...)
			}
		default:
			var b [utf8.UTFMax]byte
			n := utf8.EncodeRune(b[:], r)
			dst = append(dst, b[:n]...)
		}
	}
	return append(dst, '"')
}

func surrogates(r rune) (rune, rune) {
	r -= 0x10000
	return 0xd800 + (r>>10)&0x3ff, 0xdc00 + r&0x3ff
}

// Realistic documents: a Sanity article with Portable Text.

func genDoc(blocks int) map[string]interface{} {
	rng := rand.New(rand.NewSource(int64(blocks)))
	body := make([]interface{}, blocks)
	for i := range body {
		children := make([]interface{}, 1+rng.Intn(3))
		for j := range children {
			children[j] = map[string]interface{}{
				"_type": "span",
				"_key":  fmt.Sprintf("s%06x", rng.Intn(1<<24)),
				"text":  lorem(rng, 20+rng.Intn(60)),
				"marks": []interface{}{},
			}
		}
		body[i] = map[string]interface{}{
			"_type":    "block",
			"_key":     fmt.Sprintf("b%06x", rng.Intn(1<<24)),
			"style":    []string{"normal", "h2", "blockquote"}[rng.Intn(3)],
			"markDefs": []interface{}{},
			"children": children,
		}
	}
	doc := map[string]interface{}{
		"_id":        "drafts.0f5c3c9e-7a7b-4f0c-9a59-5c0b9c3a0d3e",
		"_type":      "article",
		"_createdAt": "2026-01-02T03:04:05Z",
		"_updatedAt": "2026-01-02T03:04:05Z",
		"title":      lorem(rng, 8),
		"slug":       map[string]interface{}{"_type": "slug", "current": "a-title"},
		"author":     map[string]interface{}{"_type": "reference", "_ref": "author-1"},
		"tags":       []interface{}{"go", "json", "performance"},
		"body":       body,
	}
	return normalizeDoc(doc)
}

// editDoc types a word into the middle of the document and bumps _updatedAt,
// like a Studio edit.
func editDoc(doc map[string]interface{}) map[string]interface{} {
	out := normalizeDoc(doc)
	out["_updatedAt"] = "2026-01-02T03:04:06Z"
	body := out["body"].([]interface{})
	block := body[len(body)/2].(map[string]interface{})
	span := block["children"].([]interface{})[0].(map[string]interface{})
	text := span["text"].(string)
	span["text"] = text[:len(text)/2] + "typed " + text[len(text)/2:]
	return out
}

func normalizeDoc(doc map[string]interface{}) map[string]interface{} {
	b, _ := json.Marshal(doc)
	var out map[string]interface{}
	_ = json.Unmarshal(b, &out)
	return out
}

func lorem(rng *rand.Rand, words int) string {
	ws := []string{"lorem", "ipsum", "dolor", "sit", "amet", "consectetur", "adipiscing", "elit", "sed", "do"}
	var b bytes.Buffer
	for i := 0; i < words; i++ {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(ws[rng.Intn(len(ws))])
	}
	return b.String()
}

// TestRawPatchRandomOps runs random op sequences, including ops the differ
// never emits, through ApplyPatch and RawPatcher. Wherever ApplyPatch
// succeeds, RawPatcher must give the same result.
func TestRawPatchRandomOps(t *testing.T) {
	n := 200000
	if testing.Short() {
		n = 20000
	}
	var p mendoza.RawPatcher
	ok := 0
	for seed := 0; seed < n; seed++ {
		rng := rand.New(rand.NewSource(int64(seed)))
		left := genValue(rng, 4)
		patch := randomPatch(rng)
		want, err := tryApply(left, patch)
		if err != nil {
			continue
		}
		ok++
		patchJSON, err := json.Marshal(patch)
		require.NoError(t, err)
		for _, style := range styles {
			doc := encode(nil, left, style, rng)
			got, err := p.Apply(nil, doc, patchJSON)
			require.NoError(t, err, "seed %d style %s\ndoc:   %s\npatch: %s", seed, style.name, doc, patchJSON)
			require.Equal(t, normalize(t, want), normalize(t, decode(t, got)),
				"seed %d style %s\ndoc:   %s\npatch: %s\ngot:   %s", seed, style.name, doc, patchJSON, got)
		}
	}
	t.Logf("%d of %d random patches applied", ok, n)
}

func tryApply(left interface{}, patch mendoza.Patch) (v interface{}, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return mendoza.MaybeApplyPatch(left, patch)
}

func randomPatch(rng *rand.Rand) mendoza.Patch {
	n := 1 + rng.Intn(8)
	patch := make(mendoza.Patch, n)
	idx := func() int { return rng.Intn(4) }
	for i := range patch {
		var op mendoza.Op
		switch rng.Intn(24) {
		case 0:
			op = &mendoza.OpValue{Value: genValue(rng, 2)}
		case 1:
			op = &mendoza.OpCopy{}
		case 2:
			op = &mendoza.OpBlank{}
		case 3:
			op = &mendoza.OpReturnIntoArray{}
		case 4:
			op = &mendoza.OpReturnIntoObject{Key: keyAlphabet[rng.Intn(len(keyAlphabet))]}
		case 5:
			op = &mendoza.OpReturnIntoObjectSameKey{}
		case 6:
			op = &mendoza.OpPushField{Index: idx()}
		case 7:
			op = &mendoza.OpPushElement{Index: idx()}
		case 8:
			op = &mendoza.OpPushParent{N: rng.Intn(2)}
		case 9:
			op = &mendoza.OpPop{}
		case 10:
			op = &mendoza.OpPushFieldCopy{OpPushField: mendoza.OpPushField{Index: idx()}}
		case 11:
			op = &mendoza.OpPushFieldBlank{OpPushField: mendoza.OpPushField{Index: idx()}}
		case 12:
			op = &mendoza.OpPushElementCopy{OpPushElement: mendoza.OpPushElement{Index: idx()}}
		case 13:
			op = &mendoza.OpPushElementBlank{OpPushElement: mendoza.OpPushElement{Index: idx()}}
		case 14:
			op = &mendoza.OpReturnIntoObjectPop{OpReturnIntoObject: mendoza.OpReturnIntoObject{Key: keyAlphabet[rng.Intn(len(keyAlphabet))]}}
		case 15:
			op = &mendoza.OpReturnIntoObjectSameKeyPop{}
		case 16:
			op = &mendoza.OpReturnIntoArrayPop{}
		case 17:
			op = &mendoza.OpObjectSetFieldValue{OpValue: mendoza.OpValue{Value: genValue(rng, 1)}, OpReturnIntoObject: mendoza.OpReturnIntoObject{Key: keyAlphabet[rng.Intn(len(keyAlphabet))]}}
		case 18:
			op = &mendoza.OpObjectCopyField{OpPushField: mendoza.OpPushField{Index: idx()}}
		case 19:
			op = &mendoza.OpObjectDeleteField{Index: idx()}
		case 20:
			op = &mendoza.OpArrayAppendValue{Value: genValue(rng, 1)}
		case 21:
			l := idx()
			op = &mendoza.OpArrayAppendSlice{Left: l, Right: l + rng.Intn(3)}
		case 22:
			op = &mendoza.OpStringAppendString{String: genString(rng)}
		default:
			l := rng.Intn(6)
			op = &mendoza.OpStringAppendSlice{Left: l, Right: l + rng.Intn(6)}
		}
		patch[i] = op
	}
	return patch
}
