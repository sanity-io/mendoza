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

// Applies a patch to a document. Returns an error if the patch
// cannot be applied (e.g., the document structure doesn't match).
//
// This function uses the default options.
func ApplyPatch(root interface{}, patch Patch) (interface{}, error) {
	return DefaultOptions.ApplyPatch(root, patch)
}

// MustApplyPatch applies a patch to a document. It panics if the patch
// cannot be applied (e.g., the document structure doesn't match).
//
// This function uses the default options.
func MustApplyPatch(root interface{}, patch Patch) interface{} {
	return DefaultOptions.MustApplyPatch(root, patch)
}

// Applies a patch to a document. Returns an error if the patch
// cannot be applied (e.g., the document structure doesn't match).
func (options *Options) ApplyPatch(root interface{}, patch Patch) (interface{}, error) {
	if len(patch) == 0 {
		return root, nil
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
		if err := op.applyTo(&p); err != nil {
			return nil, err
		}
	}

	return p.result(), nil
}

// MustApplyPatch applies a patch to a document. It panics if the patch
// cannot be applied (e.g., the document structure doesn't match).
func (options *Options) MustApplyPatch(root interface{}, patch Patch) interface{} {
	result, err := options.ApplyPatch(root, patch)
	if err != nil {
		panic(err)
	}
	return result
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

func (entry *inputEntry) getField(idx int) (fieldEntry, error) {
	if entry.fields == nil {
		obj, ok := entry.value.(map[string]interface{})
		if !ok {
			return fieldEntry{}, ErrInvalidPatch
		}
		fields := []fieldEntry{}
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

	if idx < 0 || idx >= len(entry.fields) {
		return fieldEntry{}, ErrInvalidPatch
	}

	return entry.fields[idx], nil
}

func (patcher *patcher) inputObject() (map[string]interface{}, error) {
	obj, ok := patcher.inputEntry().value.(map[string]interface{})
	if !ok {
		return nil, ErrInvalidPatch
	}
	return obj, nil
}

func (patcher *patcher) inputArray() ([]interface{}, error) {
	arr, ok := patcher.inputEntry().value.([]interface{})
	if !ok {
		return nil, ErrInvalidPatch
	}
	return arr, nil
}

func (patcher *patcher) inputString() (string, error) {
	str, ok := patcher.inputEntry().value.(string)
	if !ok {
		return "", ErrInvalidPatch
	}
	return str, nil
}

func (patcher *patcher) result() interface{} {
	entry := patcher.outputStack[len(patcher.outputStack)-1]
	return entry.result()
}

func (patcher *patcher) outputObject() (map[string]interface{}, error) {
	entry := &patcher.outputStack[len(patcher.outputStack)-1]

	if entry.writableObject == nil {
		if entry.source == nil {
			entry.writableObject = make(map[string]interface{})
		} else {
			src, ok := entry.source.(map[string]interface{})
			if !ok {
				return nil, ErrInvalidPatch
			}
			obj := make(map[string]interface{}, len(src))

			for k, v := range src {
				obj[k] = v
			}
			entry.writableObject = obj
		}
	}

	return entry.writableObject, nil
}

func (patcher *patcher) outputArray() (*[]interface{}, error) {
	entry := &patcher.outputStack[len(patcher.outputStack)-1]

	if entry.source != nil {
		src, ok := entry.source.([]interface{})
		if !ok {
			return nil, ErrInvalidPatch
		}
		entry.writableArray = make([]interface{}, len(src))
		copy(entry.writableArray, src)
		entry.source = nil
	}

	return &entry.writableArray, nil
}

func (patcher *patcher) outputString() (*string, error) {
	entry := &patcher.outputStack[len(patcher.outputStack)-1]

	if entry.source != nil {
		src, ok := entry.source.(string)
		if !ok {
			return nil, ErrInvalidPatch
		}
		entry.writableString = src
		entry.source = nil
	}

	return &entry.writableString, nil
}

func (op OpValue) applyTo(p *patcher) error {
	p.outputStack = append(p.outputStack, outputEntry{
		source: op.Value,
	})
	return nil
}

func (op OpCopy) applyTo(p *patcher) error {
	input := p.inputEntry()
	p.outputStack = append(p.outputStack, outputEntry{
		source: input.value,
	})
	return nil
}

func (op OpBlank) applyTo(p *patcher) error {
	p.outputStack = append(p.outputStack, outputEntry{
		source: nil,
	})
	return nil
}

func (op OpReturnIntoObject) applyTo(p *patcher) error {
	result := p.outputEntry().result()
	p.popOutput()
	obj, err := p.outputObject()
	if err != nil {
		return err
	}
	obj[op.Key] = result
	return nil
}

func (op OpReturnIntoObjectSameKey) applyTo(p *patcher) error {
	key := p.inputEntry().key
	result := p.outputEntry().result()
	p.popOutput()
	obj, err := p.outputObject()
	if err != nil {
		return err
	}
	obj[key] = result
	return nil
}

func (op OpReturnIntoArray) applyTo(p *patcher) error {
	result := p.outputEntry().result()
	p.popOutput()
	arr, err := p.outputArray()
	if err != nil {
		return err
	}
	*arr = append(*arr, result)
	return nil
}

func (op OpPushField) applyTo(p *patcher) error {
	field, err := p.inputEntry().getField(op.Index)
	if err != nil {
		return err
	}
	value := field.value
	if p.options.convertFunc != nil {
		value = p.options.convertFunc(value)
	}
	p.inputStack = append(p.inputStack, inputEntry{
		key:   field.key,
		value: value,
	})
	return nil
}

func (op OpPushElement) applyTo(p *patcher) error {
	arr, err := p.inputArray()
	if err != nil {
		return err
	}
	if op.Index < 0 || op.Index >= len(arr) {
		return ErrInvalidPatch
	}
	value := arr[op.Index]
	if p.options.convertFunc != nil {
		value = p.options.convertFunc(value)
	}
	p.inputStack = append(p.inputStack, inputEntry{
		value: value,
	})
	return nil
}

func (op OpPushParent) applyTo(p *patcher) error {
	idx := len(p.inputStack) - 2 - op.N
	if idx < 0 || idx >= len(p.inputStack) {
		return ErrInvalidPatch
	}
	entry := p.inputStack[idx]
	p.inputStack = append(p.inputStack, entry)
	return nil
}

func (op OpPop) applyTo(p *patcher) error {
	p.popInput()
	return nil
}

func (op OpPushFieldCopy) applyTo(p *patcher) error {
	if err := op.OpPushField.applyTo(p); err != nil {
		return err
	}
	return op.OpCopy.applyTo(p)
}

func (op OpPushFieldBlank) applyTo(p *patcher) error {
	if err := op.OpPushField.applyTo(p); err != nil {
		return err
	}
	return op.OpBlank.applyTo(p)
}

func (op OpPushElementCopy) applyTo(p *patcher) error {
	if err := op.OpPushElement.applyTo(p); err != nil {
		return err
	}
	return op.OpCopy.applyTo(p)
}

func (op OpPushElementBlank) applyTo(p *patcher) error {
	if err := op.OpPushElement.applyTo(p); err != nil {
		return err
	}
	return op.OpBlank.applyTo(p)
}

func (op OpReturnIntoObjectPop) applyTo(p *patcher) error {
	if err := op.OpReturnIntoObject.applyTo(p); err != nil {
		return err
	}
	return op.OpPop.applyTo(p)
}

func (op OpReturnIntoObjectSameKeyPop) applyTo(p *patcher) error {
	if err := op.OpReturnIntoObjectSameKey.applyTo(p); err != nil {
		return err
	}
	return op.OpPop.applyTo(p)
}

func (op OpReturnIntoArrayPop) applyTo(p *patcher) error {
	if err := op.OpReturnIntoArray.applyTo(p); err != nil {
		return err
	}
	return op.OpPop.applyTo(p)
}

func (op OpObjectSetFieldValue) applyTo(p *patcher) error {
	if err := op.OpValue.applyTo(p); err != nil {
		return err
	}
	return op.OpReturnIntoObject.applyTo(p)
}

func (op OpObjectCopyField) applyTo(p *patcher) error {
	if err := op.OpPushField.applyTo(p); err != nil {
		return err
	}
	if err := op.OpCopy.applyTo(p); err != nil {
		return err
	}
	if err := op.OpReturnIntoObjectSameKey.applyTo(p); err != nil {
		return err
	}
	return op.OpPop.applyTo(p)
}

func (op OpObjectDeleteField) applyTo(p *patcher) error {
	field, err := p.inputEntry().getField(op.Index)
	if err != nil {
		return err
	}
	obj, err := p.outputObject()
	if err != nil {
		return err
	}
	delete(obj, field.key)
	return nil
}

func (op OpArrayAppendValue) applyTo(p *patcher) error {
	arr, err := p.outputArray()
	if err != nil {
		return err
	}
	*arr = append(*arr, op.Value)
	return nil
}

func (op OpArrayAppendSlice) applyTo(p *patcher) error {
	src, err := p.inputArray()
	if err != nil {
		return err
	}
	if op.Left < 0 || op.Right > len(src) || op.Left > op.Right {
		return ErrInvalidPatch
	}
	arr, err := p.outputArray()
	if err != nil {
		return err
	}
	*arr = append(*arr, src[op.Left:op.Right]...)
	return nil
}

func (op OpStringAppendString) applyTo(p *patcher) error {
	str, err := p.outputString()
	if err != nil {
		return err
	}
	*str = *str + op.String
	return nil
}

func (op OpStringAppendSlice) applyTo(p *patcher) error {
	src, err := p.inputString()
	if err != nil {
		return err
	}
	if op.Left < 0 || op.Right > len(src) || op.Left > op.Right {
		return ErrInvalidPatch
	}
	str, err := p.outputString()
	if err != nil {
		return err
	}
	*str = *str + src[op.Left:op.Right]
	return nil
}
