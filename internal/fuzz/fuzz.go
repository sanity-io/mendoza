package fuzz

import (
	"bytes"
	"encoding/json"
	"github.com/sanity-io/mendoza"
	"reflect"
)

func Fuzz(data []byte) int {
	dec := json.NewDecoder(bytes.NewReader(data))
	var left, right interface{}

	err := dec.Decode(&left)
	if err != nil {
		return -1
	}

	err = dec.Decode(&right)
	if err != nil {
		return -1
	}

	patch1, patch2, err := mendoza.CreateDoublePatch(left, right)
	if err != nil {
		panic(err)
	}

	constructedRight := mendoza.ApplyPatch(left, patch1)
	if !reflect.DeepEqual(right, constructedRight) {
		panic("up patch is incorrect")
	}

	constructedLeft := mendoza.ApplyPatch(right, patch2)
	if !reflect.DeepEqual(left, constructedLeft) {
		panic("down patch is incorrect")
	}

	return 0
}
