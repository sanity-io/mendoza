package mendoza_test

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sanity-io/mendoza"
)

type exactDiffEntry struct {
	path []string
	val  interface{}
}

type exactDiffReporter struct {
	entries []exactDiffEntry

	currentPath []string
}

func (r *exactDiffReporter) EnterField(key string) {
	r.currentPath = append(r.currentPath, key)
}

func (r *exactDiffReporter) LeaveField(key string) {
	r.currentPath = r.currentPath[:len(r.currentPath)-1]
}

func (r *exactDiffReporter) EnterElement(idx int) {
	r.currentPath = append(r.currentPath, strconv.Itoa(idx))
}

func (r *exactDiffReporter) LeaveElement(_ int) {
	r.currentPath = r.currentPath[:len(r.currentPath)-1]
}

func (r *exactDiffReporter) Report(val interface{}) {
	entry := exactDiffEntry{
		path: make([]string, len(r.currentPath)),
		val:  val,
	}
	copy(entry.path, r.currentPath)
	r.entries = append(r.entries, entry)
}

func TestExactDiff(t *testing.T) {
	type testCase struct {
		name   string
		left   interface{}
		right  interface{}
		result []exactDiffEntry
	}

	for _, tc := range []testCase{
		{
			name:   "float no diff",
			left:   map[string]interface{}{"a": 1.0, "b": 2.0, "c": 3.0, "d": 4.0},
			right:  map[string]interface{}{"a": 1.0, "b": 2.0, "c": 3.0, "d": 4.0},
			result: []exactDiffEntry{},
		},
		{
			name:  "float single field diff",
			left:  map[string]interface{}{"a": 1.0, "b": 3.0, "c": 3.0, "d": 4.0},
			right: map[string]interface{}{"a": 1.0, "b": 2.0, "c": 3.0, "d": 4.0},
			result: []exactDiffEntry{
				{
					path: []string{"b"},
					val:  2.0,
				},
			},
		},
		{
			name:  "map changes values",
			left:  map[string]interface{}{"a": 1.0, "b": 2.0},
			right: map[string]interface{}{"a": 1.0, "b": map[string]interface{}{"c": 3.0, "d": 4.0}},
			result: []exactDiffEntry{
				{
					path: []string{"b", "c"},
					val:  3.0,
				},
				{
					path: []string{"b", "d"},
					val:  4.0,
				},
			},
		},
		{
			name:  "map update",
			left:  map[string]interface{}{"a": 1.0},
			right: map[string]interface{}{"a": 1.0, "b": map[string]interface{}{"c": []interface{}{0.0}}},
			result: []exactDiffEntry{
				{
					path: []string{"b"},
					val:  map[string]interface{}{"c": []interface{}{0.0}},
				},
			},
		},
		{
			name:   "slice no diff",
			left:   map[string]interface{}{"a": 1.0, "b": []interface{}{1.0, 2.0}},
			right:  map[string]interface{}{"a": 1.0, "b": []interface{}{1.0, 2.0}},
			result: []exactDiffEntry{},
		},
		{
			name:  "slice one element diff",
			left:  map[string]interface{}{"a": 1.0, "b": []interface{}{1.0, 2.0}},
			right: map[string]interface{}{"a": 1.0, "b": []interface{}{2.0, 2.0}},
			result: []exactDiffEntry{
				{
					path: []string{"b", "0"},
					val:  2.0,
				},
			},
		},
		{
			name:   "string no diff",
			left:   map[string]interface{}{"a": 1.0, "b": "hello", "c": 3.0, "d": 4.0},
			right:  map[string]interface{}{"a": 1.0, "b": "hello", "c": 3.0, "d": 4.0},
			result: []exactDiffEntry{},
		},
		{
			name:  "string single field diff",
			left:  map[string]interface{}{"a": 1.0, "b": "hello", "c": 3.0, "d": 4.0},
			right: map[string]interface{}{"a": 1.0, "b": "world", "c": 3.0, "d": 4.0},
			result: []exactDiffEntry{
				{
					path: []string{"b"},
					val:  "world",
				},
			},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			reporter := exactDiffReporter{entries: make([]exactDiffEntry, 0)}
			opts := mendoza.DefaultOptions.WithExactDiffReporter(&reporter)
			_, err := opts.CreatePatch(tc.left, tc.right)
			require.NoError(t, err)
			require.EqualValues(t, tc.result, reporter.entries)
		})
	}
}
