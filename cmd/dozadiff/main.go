package main

import (
	"encoding/json"
	"fmt"
	"github.com/sanity-io/mendoza"
	"os"
)

func readJson(jsonPath string) (interface{}, error) {
	jsonFile, err := os.Open(jsonPath)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(jsonFile)
	var doc interface{}
	err = decoder.Decode(&doc)
	if err != nil {
		return nil, err
	}
	return doc, nil
}

func run(leftPath, rightPath string) error {
	aDoc, err := readJson(os.Args[1])
	if err != nil {
		return err
	}
	bDoc, err := readJson(os.Args[2])
	if err != nil {
		return err
	}

	patch, err := mendoza.CreatePatch(aDoc, bDoc)
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(os.Stdout)
	err = encoder.Encode(patch)
	if err != nil {
		return err
	}

	return nil
}

func main() {
	if len(os.Args) != 3 {
		fmt.Printf("usage: dozadiff left.json right.json\n")
		return
	}

	err := run(os.Args[1], os.Args[2])
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}
