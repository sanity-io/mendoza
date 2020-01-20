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
[1,8,"name",1,"name",4,"firstName",2,"skills",9,"Diffing",10,0,2,4]
```

**Features / non-features:**

- Lightweight JSON format.
- Flexible format which can accommodate more advanced encodings in the future.
- Efficient handling of renaming of fields.
- Efficient handling of reordering of arrays.
- Not designed to be human readable.

**Format**: See [docs/format.adoc](docs/format.adoc)
