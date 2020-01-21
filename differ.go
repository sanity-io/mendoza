package mendoza

import "github.com/sanity-io/mendoza/internal/mendoza"

type differ struct {
	left      *mendoza.HashList
	right     *mendoza.HashList
	hashIndex *mendoza.HashIndex
}

func Diff(left, right interface{}) (Patch, error) {
	leftList, err := mendoza.HashListFor(left)
	if err != nil {
		return nil, err
	}
	rightList, err := mendoza.HashListFor(right)
	if err != nil {
		return nil, err
	}

	hashIndex := mendoza.NewHashIndex(leftList)
	differ := differ{
		left:      leftList,
		right:     rightList,
		hashIndex: hashIndex,
	}
	return differ.build(), nil
}

func DoubleDiff(left, right interface{}) (Patch, Patch, error) {
	leftList, err := mendoza.HashListFor(left)
	if err != nil {
		return nil, nil, err
	}
	rightList, err := mendoza.HashListFor(right)
	if err != nil {
		return nil, nil, err
	}

	leftHashIndex := mendoza.NewHashIndex(leftList)
	leftDiffer := differ{
		left:      leftList,
		right:     rightList,
		hashIndex: leftHashIndex,
	}

	rightHashIndex := mendoza.NewHashIndex(rightList)
	rightDiffer := differ{
		left:      rightList,
		right:     leftList,
		hashIndex: rightHashIndex,
	}
	return leftDiffer.build(), rightDiffer.build(), nil
}

func (d *differ) build() Patch {
	maker := rootMaker{}
	d.diffAtIndex(&maker, 0, 0)
	return maker.patch
}

func (d *differ) diffAtIndex(m PatchMaker, leftIdx, rightIdx int) {
	leftEntry := d.left.Entries[leftIdx]
	rightEntry := d.right.Entries[rightIdx]

	if leftEntry.Hash == rightEntry.Hash {
		m.Enter(EnterCopy)
		m.Leave()
		return
	}

	if d.left.IsNonEmptyMap(leftIdx) && d.right.IsNonEmptyMap(rightIdx) {
		d.diffMaps(m, leftIdx, rightIdx)
		return
	}

	if d.left.IsNonEmptySlice(leftIdx) && d.right.IsNonEmptySlice(rightIdx) {
		d.diffSlices(m, leftIdx, rightIdx)
		return
	}

	m.SetValue(rightEntry.Value)
}

/// Map differ

func (d *differ) diffMaps(m PatchMaker, leftIdx, rightIdx int) {
	md := mapDiffer{
		differ:   d,
		maker:    m,
		leftIdx:  leftIdx,
		rightIdx: rightIdx,
		copies:   map[string]struct{}{},
		extra:    map[string]Patch{},
	}

	md.process()
	md.finalize()
}

type mapDiffer struct {
	differ   *differ
	maker    PatchMaker
	leftIdx  int
	rightIdx int

	// Fields that can be copied directly
	copies map[string]struct{}
	// Fields we custom patches
	extra map[string]Patch
}

func (md *mapDiffer) process() {
	leftIter := md.differ.left.Iter(md.leftIdx)
	rightIter := md.differ.right.Iter(md.rightIdx)

	for ; !rightIter.IsDone(); rightIter.Next() {
		md.processField(leftIter, rightIter)
	}
}

func (md *mapDiffer) processField(leftIter *mendoza.Iter, rightIter *mendoza.Iter) {
	childEntry := rightIter.GetEntry()
	childKey := childEntry.Reference.Key

	// First look for an exact equal value:
	for _, otherIdx := range md.differ.hashIndex.Data[childEntry.Hash] {
		otherEntry := md.differ.left.Entries[otherIdx]
		if otherEntry.Parent != md.leftIdx {
			// Currently we don't care about values in different locations.
			continue
		}

		if otherEntry.Reference.Key == childKey {
			md.copies[childKey] = struct{}{}
		} else {
			// Copy with different name
			md.extra[childKey] = Patch{
				OpEnterField{EnterCopy, otherEntry.Reference.Key},
				OpReturn{childKey},
			}
		}

		return
	}

	// At this point we weren't able to find an exact similar value.
	// Let's iterate the left side and look for the same key there.

	rightChildIdx := rightIter.GetIndex()
	leftChildIdx := -1

	for !leftIter.IsDone() {
		leftKey := leftIter.GetKey()

		if leftKey == childKey {
			leftChildIdx = leftIter.GetIndex()
			leftIter.Next()
			break
		}

		if leftKey > childKey {
			// We went too far.
			break
		}

		leftIter.Next()
	}

	var patch Patch

	if leftChildIdx != -1 {
		m := nestedMaker{key: childKey}
		md.differ.diffAtIndex(&m, leftChildIdx, rightChildIdx)
		patch = m.patch
	} else {
		patch = Patch{
			OpSetFieldValue{
				Key:   childKey,
				Value: childEntry.Value,
			},
		}
	}

	md.extra[childKey] = patch
}

func (md *mapDiffer) finalize() {
	keepCount := 0
	// overwriteCount := 0
	deletes := map[string]struct{}{}

	// Go over every value in the left side.
	for it := md.differ.left.Iter(md.leftIdx); !it.IsDone(); it.Next() {
		key := it.GetKey()

		if _, ok := md.copies[key]; ok {
			keepCount++
			continue
		}

		if _, ok := md.extra[key]; ok {
			// overwriteCount++
			continue
		}

		deletes[key] = struct{}{}
	}

	if keepCount >= len(deletes) {
		// We keep more than we delete: Copy the object and delete extra fields.
		md.maker.Enter(EnterCopy)
		for deleteKey := range deletes {
			md.maker.Add(OpDeleteField{deleteKey})
		}
	} else {
		// We delete more than we keep: Start with a blank object.
		md.maker.Enter(EnterObject)
		for key := range md.copies {
			md.maker.Add(OpCopyField{key})
		}
	}

	for _, nestedOps := range md.extra {
		md.maker.Add(nestedOps...)
	}

	md.maker.Leave()

	return
}

/// Slice differ

func sliceAdd(op *OpAppendSlice, idx int) Op {
	if op.Left == -1 {
		op.Left = idx
		op.Right = idx + 1
		return nil
	}

	if op.Right == idx {
		op.Right++
		return nil
	}

	result := *op
	op.Left = idx
	op.Right = idx + 1
	return result
}

func sliceFinalize(op *OpAppendSlice) Op {
	if op.Left == -1 {
		return nil
	}

	result := *op
	op.Left = -1
	op.Right = -1
	return result
}

func (d *differ) diffSlices(m PatchMaker, leftIdx, rightIdx int) {
	sliceOp := OpAppendSlice{
		Left:  -1,
		Right: -1,
	}

	m.Enter(EnterArray)

	for it := d.right.Iter(rightIdx); !it.IsDone(); it.Next() {
		childEntry := it.GetEntry()

		isDone := false

		for _, otherIdx := range d.hashIndex.Data[childEntry.Hash] {
			otherEntry := d.left.Entries[otherIdx]
			if otherEntry.Parent != leftIdx {
				// We don't care about elements in different locations.
				continue
			}

			op := sliceAdd(&sliceOp, otherEntry.Reference.Index)
			if op != nil {
				m.Add(op)
			}

			isDone = true
			break
		}

		if !isDone {
			op := sliceFinalize(&sliceOp)
			if op != nil {
				m.Add(op)
			}

			m.Add(OpAppendValue{childEntry.Value})
		}
	}

	op := sliceFinalize(&sliceOp)
	if op != nil {
		m.Add(op)
	}

	m.Leave()
}
