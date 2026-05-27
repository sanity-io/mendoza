package main

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// writeTempJSON writes contents to a unique temp file in t.TempDir() and
// returns its full path. The file is automatically removed by the test
// framework when the test finishes (TempDir cleanup).
func writeTempJSON(t *testing.T, name, contents string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	require.NoError(t, ioutil.WriteFile(p, []byte(contents), 0o644))
	return p
}

// TestReadJsonSuccess exercises the happy path of readJson: open a real file,
// decode its JSON contents, return the resulting interface{}.
func TestReadJsonSuccess(t *testing.T) {
	p := writeTempJSON(t, "doc.json", `{"a":1,"b":"two","c":[1,2,3]}`)

	got, err := readJson(p)
	require.NoError(t, err)

	want := map[string]interface{}{
		"a": float64(1),
		"b": "two",
		"c": []interface{}{float64(1), float64(2), float64(3)},
	}
	require.EqualValues(t, want, got)
}

// TestReadJsonMissingFile exercises the os.Open error branch.
func TestReadJsonMissingFile(t *testing.T) {
	_, err := readJson(filepath.Join(t.TempDir(), "does-not-exist.json"))
	require.Error(t, err)
}

// TestReadJsonInvalidJSON exercises the decoder.Decode error branch.
func TestReadJsonInvalidJSON(t *testing.T) {
	p := writeTempJSON(t, "broken.json", `{not json`)
	_, err := readJson(p)
	require.Error(t, err)
}

// captureStdout redirects os.Stdout for the duration of fn and returns
// everything that was written. Used so `run` doesn't pollute test output and
// so we can verify the JSON-encoded patch.
func captureStdout(t *testing.T, fn func()) []byte {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	old := os.Stdout
	os.Stdout = w
	defer func() {
		os.Stdout = old
	}()

	done := make(chan []byte)
	go func() {
		b, _ := ioutil.ReadAll(r)
		done <- b
	}()

	fn()
	require.NoError(t, w.Close())
	return <-done
}

// TestRunSuccess exercises the happy path of run: read two JSON files,
// create a patch, encode it to stdout. The captured stdout must parse back
// into a patch shape (top-level JSON array).
func TestRunSuccess(t *testing.T) {
	left := writeTempJSON(t, "left.json", `{"a":"a","b":"b"}`)
	right := writeTempJSON(t, "right.json", `{"a":"a","b":"c","d":"d"}`)

	var out []byte
	captured := captureStdout(t, func() {
		err := run(left, right)
		require.NoError(t, err)
	})
	out = captured

	// Output must be a JSON array (the patch).
	var parsed []interface{}
	require.NoError(t, json.Unmarshal(out, &parsed))
	require.NotEmpty(t, parsed)
}

// TestRunLeftMissing exercises the first readJson error branch in run.
func TestRunLeftMissing(t *testing.T) {
	right := writeTempJSON(t, "right.json", `{}`)
	err := run(filepath.Join(t.TempDir(), "nope.json"), right)
	require.Error(t, err)
}

// TestRunRightMissing exercises the second readJson error branch in run.
func TestRunRightMissing(t *testing.T) {
	left := writeTempJSON(t, "left.json", `{}`)
	err := run(left, filepath.Join(t.TempDir(), "nope.json"))
	require.Error(t, err)
}
