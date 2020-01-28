# Fuzzing of Mendoza

Install [`go-fuzz`](https://github.com/dvyukov/go-fuzz) and download corpus:

```
$ GO111MODULE=off go get -u github.com/dvyukov/go-fuzz/go-fuzz github.com/dvyukov/go-fuzz/go-fuzz-build
$ GO111MODULE=off go get -d github.com/dvyukov/go-fuzz-corpus
```

Copy the JSON corpus into this directory:

```
$ cp -R ${GOPATH:-$HOME/go}/src/github.com/dvyukov/go-fuzz-corpus/json/corpus ./corpus
```

Build the fuzzer:

```
$ go-fuzz-build
```

Run the fuzzer:

```
$ go-fuzz
```