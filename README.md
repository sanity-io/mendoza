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

`CreatePatch` and `CreateDoublePatch` allocate new working memory on every call.
To diff many documents, reuse a `Differ`.
The zero value uses the default options, and `Options.NewDiffer` creates one with custom options.

`d.Diff` followed by `d.WriteTo` writes the patch straight to a `mendoza.Writer` without building a `Patch`.
`Writer` is Mendoza's serialization interface, not `io.Writer`, and the library doesn't export an implementation, so you provide one for your output format.

```go
var d mendoza.Differ
for _, pair := range pairs {
    if err := d.Diff(pair.Left, pair.Right); err != nil {
        return err
    }
    if err := d.WriteTo(pair.Out); err != nil { // pair.Out implements mendoza.Writer.
        return err
    }
}
```

On Go 1.24+, warmed-up diffs then run without allocating, apart from anything your writer or convert function allocates.
The result is valid until the next diff.
Don't copy a `Differ`, use it from several goroutines at once, or mutate the inputs before writing.
Its buffers keep the capacity of the largest document it has diffed, so drop the `Differ` to release that memory.

`d.CreatePatch` and `d.CreateDoublePatch` also reuse the working memory, but still return a `Patch`.
`d.DiffDouble` followed by `d.WriteReverseTo` writes the reverse patch as well.

Object fields are deleted in key order.
Before `Differ` was added they were deleted in random order, so older patches can differ in that order while producing the same result.
