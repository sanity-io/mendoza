package mendoza

// opRec is an op recorded on the tape. a/b hold integer operands, key holds a field key or string payload,
// and val holds a literal value. The opcode determines which operands are used.
type opRec struct {
	code uint8
	a, b int
	key  string
	val  interface{}
}

// codeInclude is only used on the tape: it stands for all the ops in tape[a:b].
// Included spans are recorded before their parent patch, so these references cannot form cycles.
const codeInclude uint8 = 255

func (d *differ) emit(op opRec) {
	d.tape = append(d.tape, op)
}

// truncateTape discards a suffix, clearing document references before reusing its capacity.
func (d *differ) truncateTape(start int) {
	discarded := d.tape[start:]
	for i := range discarded {
		discarded[i] = opRec{}
	}
	d.tape = d.tape[:start]
}

func (d *differ) valueSpan(value interface{}) span {
	start := len(d.tape)
	d.emit(opRec{code: codeValue, val: value})
	return span{start, len(d.tape)}
}

// writeSpan writes the ops in tape[sp.start:sp.end] in the same format as Patch.WriteTo.
func (d *differ) writeSpan(w Writer, sp span) error {
	for i := sp.start; i < sp.end; i++ {
		op := &d.tape[i]

		if op.code == codeInclude {
			if err := d.writeSpan(w, span{op.a, op.b}); err != nil {
				return err
			}
			continue
		}

		if err := w.WriteUint8(op.code); err != nil {
			return err
		}

		var err error
		switch op.code {
		case codeBlank, codeReturnIntoObjectSameKeyPop, codeReturnIntoArrayPop:
			// These operations have no operands.
		case codeValue, codeArrayAppendValue:
			err = w.WriteValue(op.val)
		case codePushFieldBlank, codePushFieldCopy, codePushElementBlank, codePushElementCopy,
			codeObjectCopyField, codeObjectDeleteField:
			err = w.WriteUint(op.a)
		case codeReturnIntoObjectPop, codeStringAppendString:
			err = w.WriteString(op.key)
		case codeObjectSetFieldValue:
			if err = w.WriteValue(op.val); err == nil {
				err = w.WriteString(op.key)
			}
		case codeArrayAppendSlice, codeStringAppendSlice:
			if err = w.WriteUint(op.a); err == nil {
				err = w.WriteUint(op.b)
			}
		default:
			panic("mendoza: unexpected op on tape")
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// patchFor converts the ops in tape[sp.start:sp.end] into a Patch.
func (d *differ) patchFor(sp span) Patch {
	// Count first so the returned slice needs only one allocation. Includes can expand to many ops.
	return d.appendOps(make(Patch, 0, d.countOps(sp)), sp)
}

func (d *differ) countOps(sp span) int {
	count := 0
	for i := sp.start; i < sp.end; i++ {
		if op := &d.tape[i]; op.code == codeInclude {
			count += d.countOps(span{op.a, op.b})
		} else {
			count++
		}
	}
	return count
}

func (d *differ) appendOps(patch Patch, sp span) Patch {
	for i := sp.start; i < sp.end; i++ {
		if op := &d.tape[i]; op.code == codeInclude {
			patch = d.appendOps(patch, span{op.a, op.b})
		} else {
			patch = append(patch, op.toOp())
		}
	}
	return patch
}

func (op *opRec) toOp() Op {
	switch op.code {
	case codeValue:
		return &OpValue{Value: op.val}
	case codeBlank:
		return &OpBlank{}
	case codePushFieldCopy:
		return &OpPushFieldCopy{OpPushField: OpPushField{Index: op.a}}
	case codePushFieldBlank:
		return &OpPushFieldBlank{OpPushField: OpPushField{Index: op.a}}
	case codePushElementCopy:
		return &OpPushElementCopy{OpPushElement: OpPushElement{Index: op.a}}
	case codePushElementBlank:
		return &OpPushElementBlank{OpPushElement: OpPushElement{Index: op.a}}
	case codeReturnIntoObjectPop:
		return &OpReturnIntoObjectPop{OpReturnIntoObject: OpReturnIntoObject{Key: op.key}}
	case codeReturnIntoObjectSameKeyPop:
		return &OpReturnIntoObjectSameKeyPop{}
	case codeReturnIntoArrayPop:
		return &OpReturnIntoArrayPop{}
	case codeObjectSetFieldValue:
		return &OpObjectSetFieldValue{OpValue: OpValue{Value: op.val}, OpReturnIntoObject: OpReturnIntoObject{Key: op.key}}
	case codeObjectCopyField:
		return &OpObjectCopyField{OpPushField: OpPushField{Index: op.a}}
	case codeObjectDeleteField:
		return &OpObjectDeleteField{Index: op.a}
	case codeArrayAppendValue:
		return &OpArrayAppendValue{Value: op.val}
	case codeArrayAppendSlice:
		return &OpArrayAppendSlice{Left: op.a, Right: op.b}
	case codeStringAppendString:
		return &OpStringAppendString{String: op.key}
	case codeStringAppendSlice:
		return &OpStringAppendSlice{Left: op.a, Right: op.b}
	}
	panic("mendoza: unexpected op on tape")
}
