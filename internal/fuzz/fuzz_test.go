package fuzz

import (
	"testing"
)

// TestFuzzInvalidUTF8 exercises the early-return branch in Fuzz that rejects
// byte slices containing invalid UTF-8 sequences (fuzz.go:46-48).
func TestFuzzInvalidUTF8(t *testing.T) {
	// 0xff is never a valid leading UTF-8 byte.
	got := Fuzz([]byte{0xff, 0xfe, 0xfd})
	if got != -1 {
		t.Fatalf("expected -1 for invalid UTF-8 input, got %d", got)
	}
}

// TestFuzzEmpty exercises the "first json.Decode fails" branch (fuzz.go:53-56)
// by passing an empty (but valid-UTF-8) input that produces io.EOF.
func TestFuzzEmpty(t *testing.T) {
	got := Fuzz([]byte{})
	if got != -1 {
		t.Fatalf("expected -1 for empty input, got %d", got)
	}
}

// TestFuzzMalformedJSON exercises the same "first json.Decode fails" branch
// but with a syntactically broken JSON token rather than EOF.
func TestFuzzMalformedJSON(t *testing.T) {
	got := Fuzz([]byte("{not-json"))
	if got != -1 {
		t.Fatalf("expected -1 for malformed JSON, got %d", got)
	}
}

// TestFuzzOnlyOneValue exercises the "second json.Decode fails" branch
// (fuzz.go:58-61) when only a single JSON value is present.
func TestFuzzOnlyOneValue(t *testing.T) {
	got := Fuzz([]byte(`{"a":1}`))
	if got != -1 {
		t.Fatalf("expected -1 when only one JSON value present, got %d", got)
	}
}

// TestFuzzSecondValueMalformed exercises the "second json.Decode fails" branch
// with a malformed second value following a valid first value.
func TestFuzzSecondValueMalformed(t *testing.T) {
	got := Fuzz([]byte(`{"a":1} {not-json`))
	if got != -1 {
		t.Fatalf("expected -1 for malformed second value, got %d", got)
	}
}

// TestFuzzHappyPath drives the full body of Fuzz on a variety of (left, right)
// JSON-document pairs. Each pair is two valid JSON values separated by
// whitespace, which the decoder will read as two consecutive Decode calls.
// All pairs must roundtrip via both JSON and msgpack patch encoders without
// panicking, and Fuzz must return 0.
func TestFuzzHappyPath(t *testing.T) {
	cases := []struct {
		name string
		data string
	}{
		// Both empty objects: trivial patch, only structural.
		{"BothEmptyObjects", `{} {}`},
		// Pure string changes: only OpStringAppend* ops, no numeric OpValue.
		{"PureStringChange", `"abc" "abcdef"`},
		{"ChangeFieldStringValue", `{"k":"abc"} {"k":"abcd"}`},
		{"StringPrefixSuffix", `"abcdefghijk" "abcXYZhijk"`},
		// Object field shuffling with only string values.
		{"ObjectFieldShuffle", `{"a":"aa","b":"bb"} {"a":"aa","b":"cc"}`},
		// Deeply nested change with string leaves.
		{"NestedStringChange", `{"x":{"y":"alpha"}} {"x":{"y":"beta"}}`},
		// Identical documents: differ should produce an empty patch.
		{"IdenticalDocs", `{"a":"a","b":"b"} {"a":"a","b":"b"}`},
		// Array of strings reordered.
		{"ArrayStringReorder", `["aaa","bbb","ccc"] ["bbb","ccc","aaa"]`},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Fuzz panicked on %q: %v", tc.data, r)
				}
			}()
			got := Fuzz([]byte(tc.data))
			if got != 0 {
				t.Fatalf("expected 0 for valid pair %q, got %d", tc.data, got)
			}
		})
	}
}
