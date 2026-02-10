package mendoza

type Options struct {
	convertFunc       func(value interface{}) interface{}
	exactDiffReporter ExactDiffReporter
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

// WithExactDiffReporter registers a reporter which is invoked on every path in the right-side document which
// is not exactly the same in the left-side document.
//
// When this is used with CreateDoublePatch it will be invoked for both documents.
func (options Options) WithExactDiffReporter(exactDiffReporter ExactDiffReporter) Options {
	options.exactDiffReporter = exactDiffReporter
	return options
}

// ExactDiffReporter uses the visitor pattern for reporting exact differences (i.e. places where
// the value is not exactly the same in both versions). EnterField and EnterElement will be called
// while the diff is calculated and Report is called when the current value is determined to be different.
type ExactDiffReporter interface {
	// EnterField is called when the differ visits a field in an object.
	EnterField(key string)
	// LeaveField is called when the differ leave a field. This will always be paired up with a call to EnterField.
	LeaveField(key string)

	// EnterElement is called when the differ visits an element in an array.
	EnterElement(idx int)
	// LeaveElement is called when the differ leaves an element. This will always be paired up with a call to EnterElement.
	LeaveElement(idx int)

	// Report is invoked when the value, which is located at the path as described by EnterField and EnterElement
	// is not exactly equivalent in the left-side document.
	Report(val interface{})
}
