package mendoza

import "github.com/sanity-io/mendoza/internal/mendoza"

func hashListForBench(d interface{}) {
	var l mendoza.HashList
	var ix mendoza.HashIndex
	benchHL(&l, &ix, d)
}

var hlS mendoza.HashList
var ixS mendoza.HashIndex

func benchHL(_ *mendoza.HashList, _ *mendoza.HashIndex, d interface{}) {
	hlS.Fill(d, nil)
	ixS.Build(&hlS)
}
