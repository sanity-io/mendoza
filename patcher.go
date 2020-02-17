package mendoza

import (
	"sort"
)

type outputEntry struct {
	source         interface{}
	writableArray  []interface{}
	writableObject map[string]interface{}
	writableString string
}

type inputEntry struct {
	key    string
	value  interface{}
	fields []fieldEntry
}

type fieldEntry struct {
	key   string
	value interface{}
}

type patcher struct {
	root        interface{}
	inputStack  []inputEntry
	outputStack []outputEntry
	options     *Options
}

// Applies a patch to a document. Note that this method can panic if
// the document is not the same that was used to produce the patch.
//
// This function uses the default options.
func ApplyPatch(root interface{}, patch Patch) interface{} {
	return DefaultOptions.ApplyPatch(root, patch)
}

// Applies a patch to a document. Note that this method can panic if
// the document is not the same that was used to produce the patch.
func (options *Options) ApplyPatch(root interface{}, patch Patch) interface{} {
	if len(patch) == 0 {
		return root
	}

	if options.convertFunc != nil {
		root = options.convertFunc(root)
	}

	p := patcher{
		options:     options,
		inputStack:  []inputEntry{{value: root}},
		outputStack: []outputEntry{{source: root}},
	}

	for _, op := range patch {
		op.applyTo(&p)
	}

	return p.result()
}

func (patcher *patcher) popInput() {
	patcher.inputStack = patcher.inputStack[:len(patcher.inputStack)-1]
}

func (patcher *patcher) popOutput() {
	patcher.outputStack = patcher.outputStack[:len(patcher.outputStack)-1]
}

func (patcher *patcher) inputEntry() *inputEntry {
	if len(patcher.inputStack) == 0 {

	}

	return &patcher.inputStack[len(patcher.inputStack)-1]
}

func (patcher *patcher) outputEntry() *outputEntry {
	return &patcher.outputStack[len(patcher.outputStack)-1]
}

func (entry *outputEntry) result() interface{} {
	if entry.writableObject != nil {
		return entry.writableObject
	}

	if entry.writableArray != nil {
		return entry.writableArray
	}

	if len(entry.writableString) > 0 {
		return entry.writableString
	}

	return entry.source
}

func (entry *inputEntry) getField(idx int) fieldEntry {
	if entry.fields == nil {
		fields := []fieldEntry{}
		obj := entry.value.(map[string]interface{})
		keys := []string{}
		for key := range obj {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			val := obj[key]
			fields = append(fields, fieldEntry{
				key:   key,
				value: val,
			})
		}
		entry.fields = fields
	}

	return entry.fields[idx]
}

func (patcher *patcher) inputObject() map[string]interface{} {
	return patcher.inputEntry().value.(map[string]interface{})
}

func (patcher *patcher) inputArray() []interface{} {
	return patcher.inputEntry().value.([]interface{})
}

func (patcher *patcher) inputString() string {
	return patcher.inputEntry().value.(string)
}

func (patcher *patcher) result() interface{} {
	entry := patcher.outputStack[len(patcher.outputStack)-1]
	return entry.result()
}

func (patcher *patcher) outputObject() map[string]interface{} {
	entry := &patcher.outputStack[len(patcher.outputStack)-1]

	if entry.writableObject == nil {
		if entry.source == nil {
			entry.writableObject = make(map[string]interface{})
		} else {
			src := entry.source.(map[string]interface{})
			obj := make(map[string]interface{}, len(src))

			for k, v := range src {
				obj[k] = v
			}
			entry.writableObject = obj
		}
	}

	return entry.writableObject
}

func (patcher *patcher) outputArray() *[]interface{} {
	entry := &patcher.outputStack[len(patcher.outputStack)-1]

	if entry.source != nil {
		src := entry.source.([]interface{})
		entry.writableArray = make([]interface{}, len(src))
		copy(entry.writableArray, src)
		entry.source = nil
	}

	return &entry.writableArray
}

func (patcher *patcher) outputString() *string {
	entry := &patcher.outputStack[len(patcher.outputStack)-1]

	if entry.source != nil {
		src := entry.source.(string)
		entry.writableString = src
		entry.source = nil
	}

	return &entry.writableString
}

func (op OpValue) applyTo(p *patcher) {
	p.outputStack = append(p.outputStack, outputEntry{
		source: op.Value,
	})
}

func (op OpCopy) applyTo(p *patcher) {
	input := p.inputEntry()
	p.outputStack = append(p.outputStack, outputEntry{
		source: input.value,
	})
}

func (op OpBlank) applyTo(p *patcher) {
	p.outputStack = append(p.outputStack, outputEntry{
		source: nil,
	})
}

func (op OpReturnIntoObject) applyTo(p *patcher) {
	result := p.outputEntry().result()
	p.popOutput()
	obj := p.outputObject()
	obj[op.Key] = result
}

func (op OpReturnIntoObjectSameKey) applyTo(p *patcher) {
	key := p.inputEntry().key
	result := p.outputEntry().result()
	p.popOutput()
	obj := p.outputObject()
	obj[key] = result
}

func (op OpReturnIntoArray) applyTo(p *patcher) {
	result := p.outputEntry().result()
	p.popOutput()
	arr := p.outputArray()
	*arr = append(*arr, result)
}

func (op OpPushField) applyTo(p *patcher) {
	field := p.inputEntry().getField(op.Index)
	value := field.value
	if p.options.convertFunc != nil {
		value = p.options.convertFunc(value)
	}
	p.inputStack = append(p.inputStack, inputEntry{
		key:   field.key,
		value: value,
	})
}

func (op OpPushElement) applyTo(p *patcher) {
	value := p.inputArray()[op.Index]
	if p.options.convertFunc != nil {
		value = p.options.convertFunc(value)
	}
	p.inputStack = append(p.inputStack, inputEntry{
		value: value,
	})
}

func (op OpPushParent) applyTo(p *patcher) {
	idx := len(p.inputStack) - 2 - op.N
	entry := p.inputStack[idx]
	p.inputStack = append(p.inputStack, entry)
}

func (op OpPop) applyTo(p *patcher) {
	p.popInput()
}

func (op OpPushFieldCopy) applyTo(p *patcher) {
	op.OpPushField.applyTo(p)
	op.OpCopy.applyTo(p)
}

func (op OpPushFieldBlank) applyTo(p *patcher) {
	op.OpPushField.applyTo(p)
	op.OpBlank.applyTo(p)
}

func (op OpPushElementCopy) applyTo(p *patcher) {
	op.OpPushElement.applyTo(p)
	op.OpCopy.applyTo(p)
}

func (op OpPushElementBlank) applyTo(p *patcher) {
	op.OpPushElement.applyTo(p)
	op.OpBlank.applyTo(p)
}

func (op OpReturnIntoObjectPop) applyTo(p *patcher) {
	op.OpReturnIntoObject.applyTo(p)
	op.OpPop.applyTo(p)
}

func (op OpReturnIntoObjectSameKeyPop) applyTo(p *patcher) {
	op.OpReturnIntoObjectSameKey.applyTo(p)
	op.OpPop.applyTo(p)
}

func (op OpReturnIntoArrayPop) applyTo(p *patcher) {
	op.OpReturnIntoArray.applyTo(p)
	op.OpPop.applyTo(p)
}

func (op OpObjectSetFieldValue) applyTo(p *patcher) {
	op.OpValue.applyTo(p)
	op.OpReturnIntoObject.applyTo(p)
}

func (op OpObjectCopyField) applyTo(p *patcher) {
	op.OpPushField.applyTo(p)
	op.OpCopy.applyTo(p)
	op.OpReturnIntoObjectSameKey.applyTo(p)
	op.OpPop.applyTo(p)
}

func (op OpObjectDeleteField) applyTo(p *patcher) {
	field := p.inputEntry().getField(op.Index)
	obj := p.outputObject()
	delete(obj, field.key)
}

func (op OpArrayAppendValue) applyTo(p *patcher) {
	arr := p.outputArray()
	*arr = append(*arr, op.Value)
}

func (op OpArrayAppendSlice) applyTo(p *patcher) {
	src := p.inputArray()
	arr := p.outputArray()
	*arr = append(*arr, src[op.Left:op.Right]...)
}

func (op OpStringAppendString) applyTo(p *patcher) {
	str := p.outputString()
	*str = *str + op.String
}

func (op OpStringAppendSlice) applyTo(p *patcher) {
	src := p.inputString()
	str := p.outputString()
	*str = *str + src[op.Left:op.Right]
}
