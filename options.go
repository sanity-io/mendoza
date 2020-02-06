package mendoza

type Options struct {
	convertFunc func(value interface{}) interface{}
}

var DefaultOptions = Options{}

func (options Options) WithConvertFunc(convertFunc func(value interface{}) interface{}) Options {
	options.convertFunc = convertFunc
	return options
}
