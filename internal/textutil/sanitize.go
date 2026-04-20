package textutil

func Sanitize[T []byte | string](s T) T {
	var input []byte
	if value, ok := any(s).([]byte); ok {
		input = value
	} else {
		input = []byte(s)
	}

	var output []byte
	for _, b := range input {
		if b != 0x0a && (b < 0x20 || b > 0x7e) {
			output = append(output, '_')
		} else {
			output = append(output, b)
		}
	}

	return T(output)
}
