package mendoza

import "fmt"

type outputEntry struct {
	key            string
	source         interface{}
	writableArray  []interface{}
	writableObject map[string]interface{}
}

type inputEntry struct {
	value interface{}
}

type Decoder struct {
	root        interface{}
	inputStack  []inputEntry
	outputStack []outputEntry
}

func Decode(root interface{}, patch Patch) interface{} {
	decoder := Decoder{
		root: root,
	}

	for _, op := range patch {
		decoder.process(op)
	}

	return decoder.result()
}

func (d *Decoder) enter(enterType EnterType, value interface{}, key string) {
	d.inputStack = append(d.inputStack, inputEntry{
		value: value,
	})

	switch enterType {
	case EnterNop:
		// do nothing
	case EnterCopy:
		d.outputStack = append(d.outputStack, outputEntry{
			key:    key,
			source: value,
		})
	case EnterArray:
		d.outputStack = append(d.outputStack, outputEntry{
			key: key,
		})
	case EnterObject:
		d.outputStack = append(d.outputStack, outputEntry{
			key:            key,
			writableObject: make(map[string]interface{}),
		})
	}
}

func (d *Decoder) returnIntoField(key string) {
	d.inputStack = d.inputStack[:len(d.inputStack)-1]

	// Read the current value, then pop the stack
	entry := d.outputStack[len(d.outputStack)-1]
	d.outputStack = d.outputStack[:len(d.outputStack)-1]

	obj := d.outputObject()

	if key == "" {
		key = entry.key
	}

	obj[key] = entry.result()
}

func (d *Decoder) inputEntry() inputEntry {
	return d.inputStack[len(d.inputStack)-1]
}

func (entry *outputEntry) result() interface{} {
	if entry.writableObject != nil {
		return entry.writableObject
	}

	if entry.writableArray != nil {
		return entry.writableArray
	}

	return entry.source
}

func (d *Decoder) inputObject() map[string]interface{} {
	return d.inputEntry().value.(map[string]interface{})
}

func (d *Decoder) inputArray() []interface{} {
	return d.inputEntry().value.([]interface{})
}

func (d *Decoder) result() interface{} {
	entry := d.outputStack[len(d.outputStack)-1]
	return entry.result()
}

func (d *Decoder) outputObject() map[string]interface{} {
	entry := &d.outputStack[len(d.outputStack)-1]

	if entry.writableObject == nil {
		src := entry.source.(map[string]interface{})
		obj := make(map[string]interface{}, len(src))

		for k, v := range src {
			obj[k] = v
		}

		entry.writableObject = obj
	}

	return entry.writableObject
}

func (d *Decoder) outputArray() *[]interface{} {
	entry := &d.outputStack[len(d.outputStack)-1]

	if entry.source != nil {
		src := entry.source.([]interface{})
		entry.writableArray = make([]interface{}, len(src))
		copy(entry.writableArray, src)
		entry.source = nil
	}

	return &entry.writableArray
}

func (d *Decoder) process(op Op) {
	switch op := op.(type) {
	case OpEnterRoot:
		d.enter(op.Enter, d.root, "")
	case OpEnterField:
		obj := d.inputObject()
		value := obj[op.Key]
		d.enter(op.Enter, value, op.Key)
	case OpEnterElement:
		arr := d.inputArray()
		value := arr[op.Index]
		d.enter(op.Enter, value, "")
	case OpReturn:
		d.returnIntoField(op.Key)
	case OpSetFieldValue:
		obj := d.outputObject()
		obj[op.Key] = op.Value
	case OpCopyField:
		srcObj := d.inputObject()
		dstObj := d.outputObject()
		dstObj[op.Key] = srcObj[op.Key]
	case OpDeleteField:
		obj := d.outputObject()
		delete(obj, op.Key)
	case OpAppendValue:
		arr := d.outputArray()
		*arr = append(*arr, op.Value)
	case OpAppendSlice:
		srcArr := d.inputArray()
		dstArr := d.outputArray()
		*dstArr = append(*dstArr, srcArr[op.Left:op.Right]...)
	case OpOutputValue:
		d.outputStack = append(d.outputStack, outputEntry{
			source: op.Value,
		})
	default:
		panic(fmt.Errorf("unknown op: %#v", op))
	}
}
