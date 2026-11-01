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

## Repeated comparisons in Go

`CreatePatch` and `CreateDoublePatch` allocate fresh working memory on every call. To diff many documents, reuse a
`Differ` (the zero value uses the default options; use `options.NewDiffer()` for others), and write its result
straight to a `Writer` without building a `Patch`:

```go
var d mendoza.Differ
for _, doc := range docs {
    if err := d.Diff(doc.Left, doc.Right); err != nil {
        return err
    }
    if err := d.WriteTo(w); err != nil {
        return err
    }
}
```

On Go 1.24+ warmed-up diffs then run without allocating. The result is valid until the next diff. Don't copy a
`Differ`, share it between goroutines, or mutate the inputs before writing. `d.CreatePatch` and
`d.CreateDoublePatch` also reuse the working memory but still return a `Patch`, and `d.DiffDouble` with
`d.WriteReverseTo` writes the reverse patch too.

Patches delete object fields in key order. Earlier versions deleted them in random order, so patches created by
different versions can differ in that order while giving the same result.
