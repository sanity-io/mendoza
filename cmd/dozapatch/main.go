package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/sanity-io/mendoza"
)

func readJson(jsonPath string, data any) error {
	jsonFile, err := os.Open(jsonPath)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(jsonFile)
	return decoder.Decode(data)
}

func run(originalPath, patchPath string) error {
	var original any
	if err := readJson(originalPath, &original); err != nil {
		return err
	}

	var patch mendoza.Patch
	if err := readJson(patchPath, &patch); err != nil {
		return err
	}

	result, err := mendoza.ApplyPatch(original, patch)
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(os.Stdout)
	if err := encoder.Encode(result); err != nil {
		return err
	}

	return nil
}

func main() {
	if len(os.Args) != 3 {
		fmt.Printf("usage: dozadiff original.json patch.json\n")
		return
	}

	err := run(os.Args[1], os.Args[2])
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}
