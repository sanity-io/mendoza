package mendoza

type Options struct {
	convertFunc func(value interface{}) interface{}
}

// The default options.
var DefaultOptions = Options{}

// WithConvertFunc creates a new option object with a given convert function.
//
// The convert function is applied by CreatePatch and ApplyPatch to every value it looks at.
// This can be used to support additional types by converting it into one of the supported types.
func (options Options) WithConvertFunc(convertFunc func(value interface{}) interface{}) Options {
	options.convertFunc = convertFunc
	return options
}
