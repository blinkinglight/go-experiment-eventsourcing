package tools

import "encoding/json"

func Unmarshal[T any](data []byte) (T, error) {
	var t T
	err := json.Unmarshal(data, &t)
	return t, err
}

func MustUnmarshal[T any](data []byte) T {
	t, err := Unmarshal[T](data)
	if err != nil {
		panic(err)
	}
	return t
}
