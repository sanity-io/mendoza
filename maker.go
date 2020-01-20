package mendoza

// PatchMaker is the interface for creating patches.
type PatchMaker interface {
	Enter(enterType EnterType)
	Add(op... Op)
	Leave()

	SetValue(value interface{})
}

type rootMaker struct {
	patch Patch
}

func (m *rootMaker) Enter(enterType EnterType) {
	m.patch = append(m.patch, OpEnterRoot{enterType})
}

func (m *rootMaker) Add(op... Op) {
	m.patch = append(m.patch, op...)
}

func (m *rootMaker) Leave() {
	// nothing to do
}

func (m *rootMaker) SetValue(value interface{}) {
	m.patch = append(m.patch, OpOutputValue{value})
}

type nestedMaker struct {
	key string
	patch Patch
}

func (m *nestedMaker) Enter(enterType EnterType) {
	m.patch = append(m.patch, OpEnterField{enterType, m.key})
}

func (m *nestedMaker) Add(op... Op) {
	m.patch = append(m.patch, op...)
}

func (m *nestedMaker) Leave() {
	m.patch = append(m.patch, OpReturn{})
}

func (m *nestedMaker) SetValue(value interface{}) {
	m.patch = append(m.patch, OpSetFieldValue{m.key, value})
}
