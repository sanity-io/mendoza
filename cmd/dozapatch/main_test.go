package main

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/sanity-io/mendoza"
	"github.com/stretchr/testify/require"
)

// writeTempJSON writes contents to a unique temp file in t.TempDir() and
// returns its full path.
func writeTempJSON(t *testing.T, name, contents string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	require.NoError(t, ioutil.WriteFile(p, []byte(contents), 0o644))
	return p
}

// TestReadJsonSuccess exercises the happy path of readJson.
func TestReadJsonSuccess(t *testing.T) {
	p := writeTempJSON(t, "doc.json", `{"a":1,"b":"two"}`)
	var dst interface{}
	require.NoError(t, readJson(p, &dst))
	require.EqualValues(t, map[string]interface{}{"a": float64(1), "b": "two"}, dst)
}

// TestReadJsonMissingFile exercises the os.Open error branch.
func TestReadJsonMissingFile(t *testing.T) {
	var dst interface{}
	err := readJson(filepath.Join(t.TempDir(), "missing.json"), &dst)
	require.Error(t, err)
}

// TestReadJsonInvalidJSON exercises the decoder.Decode error branch.
func TestReadJsonInvalidJSON(t *testing.T) {
	p := writeTempJSON(t, "broken.json", `{ not json`)
	var dst interface{}
	require.Error(t, readJson(p, &dst))
}

// captureStdout redirects os.Stdout for the duration of fn and returns the
// bytes written. Used so `run` doesn't pollute test output.
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

// TestRunSuccess exercises the happy path of run end-to-end: create a real
// patch in-memory, serialize both the original document and the patch as
// JSON files, then run() reads them back, applies, and prints the result.
// The captured stdout must equal the right document we used to build the
// patch.
func TestRunSuccess(t *testing.T) {
	leftDoc := map[string]interface{}{"a": "a", "b": "b"}
	rightDoc := map[string]interface{}{"a": "a", "b": "c", "d": "d"}

	patch, err := mendoza.CreatePatch(leftDoc, rightDoc)
	require.NoError(t, err)

	leftBytes, err := json.Marshal(leftDoc)
	require.NoError(t, err)
	patchBytes, err := json.Marshal(patch)
	require.NoError(t, err)

	leftPath := writeTempJSON(t, "left.json", string(leftBytes))
	patchPath := writeTempJSON(t, "patch.json", string(patchBytes))

	out := captureStdout(t, func() {
		err := run(leftPath, patchPath)
		require.NoError(t, err)
	})

	var got interface{}
	require.NoError(t, json.Unmarshal(out, &got))
	require.EqualValues(t, rightDoc, got)
}

// TestRunOriginalMissing exercises the first readJson error path in run.
func TestRunOriginalMissing(t *testing.T) {
	// A valid empty patch file so the second readJson would succeed if reached.
	patchPath := writeTempJSON(t, "patch.json", `[]`)
	err := run(filepath.Join(t.TempDir(), "missing.json"), patchPath)
	require.Error(t, err)
}

// TestRunPatchMissing exercises the second readJson error path in run.
func TestRunPatchMissing(t *testing.T) {
	origPath := writeTempJSON(t, "orig.json", `{}`)
	err := run(origPath, filepath.Join(t.TempDir(), "missing.json"))
	require.Error(t, err)
}
