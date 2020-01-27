package main

import (
	"encoding/json"
	"fmt"
	"github.com/sanity-io/mendoza"
	"os"
)

func readJson(jsonPath string) interface{} {
	jsonFile, err := os.Open(jsonPath)
	if err != nil {
		panic(err)
	}
	decoder := json.NewDecoder(jsonFile)
	var doc interface{}
	err = decoder.Decode(&doc)
	if err != nil {
		panic(err)
	}
	return doc
}

func main() {
	if len(os.Args) <= 2 {
		fmt.Printf("usage: dozadiff left.json right.json\n")
		return
	}

	aDoc := readJson(os.Args[1])
	bDoc := readJson(os.Args[2])

	patch, err := mendoza.Diff(aDoc, bDoc)
	if err != nil {
		panic(err)
	}

	encoder := json.NewEncoder(os.Stdout)
	err = encoder.Encode(patch)
	if err != nil {
		panic(err)
	}
}
