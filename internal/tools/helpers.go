package tools

// ptr — короткий помощник для optional-полей store и args.
func ptr[T any](value T) *T { return &value }
