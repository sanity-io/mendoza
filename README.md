# Mendoza, differ for structured documents

Mendoza looks at two structured documents, referred to as _left_ and _right_, and constructs a _patch_ of the differences.
By having the left document and the patch you'll be able to recover the right document.
Mendoza is designed for creating a _minimal_ patch, not necessarily a _readable_ patch.

Example:

```
$ cat left.json
{"name": "Bob Bobson", "age": 30, "skills": ["Go", "Patching", "Playing"]}
$ cat right.json
{"firstName": "Bob Bobson", "age": 30, "skills": ["Diffing", "Go", "Patching"]}
$ dozadiff left.json right.json
[19,1,10,1,14,"firstName",11,2,20,"Diffing",21,0,2,15]
```

**Features / non-features:**

- Lightweight JSON format.
- Flexible format which can accommodate more advanced encodings in the future.
- Differ/patcher available as a Go library (this repo).
- Patcher available as a JavaScript library: [mendoza-js](https://github.com/sanity-io/mendoza-js).
- Efficient handling of renaming of fields.
- Efficient handling of reordering of arrays.
- Not designed to be human readable.
- The patch can only be applied against the exact same version.

**Format**: See [docs/format.adoc](docs/format.adoc)

## Go API and repeated comparisons

`CreatePatch` and `CreateDoublePatch` retain their existing signatures and behavior.
They are convenience wrappers around `Differ`, and return `Patch` values that can be
applied with `ApplyPatch` or serialized as before.

For repeated comparisons, reuse a `Differ`:

- `d.CreatePatch(left, right)` and `d.CreateDoublePatch(left, right)` reuse working
  memory, but still allocate the returned patches and operation objects.
- `d.Diff(left, right)` followed by `d.WriteTo(w)` writes directly from the internal
  operation tape, without constructing a `Patch`. The writer implements Mendoza's
  `Writer` interface, not `io.Writer`.
- `d.DiffDouble(left, right)` also supports `d.WriteReverseTo(w)`.

```go
d := mendoza.NewDiffer() // Or options.NewDiffer(); reuse across comparisons.
if err := d.Diff(left, right); err != nil {
    return err
}
return d.WriteTo(w)
```

A `Differ` must not be copied after first use or shared by concurrent comparisons.
Every diff replaces both previous tape results, including on error. Writes can be
repeated until the next diff. Returned `Patch` values survive reuse of the differ,
although their values may still reference input maps and slices. Do not mutate inputs
between diffing and writing.

Working buffers grow as needed and retain their capacity; zero allocations does not
mean bounded memory. Replace the differ with a new one if you need to release its
retained buffers. On Go 1.24+ with the default runtime, warmed-up diffs can run with
zero allocations. Older Go map implementations may allocate while rebuilding the
index, and custom writers or conversion functions can allocate independently.
