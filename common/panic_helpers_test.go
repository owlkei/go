package common

func recoverValue(fn func()) (any, bool) {
	var value any
	var panicked bool
	func() {
		defer func() {
			if r := recover(); r != nil {
				value = r
				panicked = true
			}
		}()
		fn()
	}()
	return value, panicked
}
