package json

import "encoding/json"

// MustMarshal marshals an object and panics if it failures. This function should only be used
// when the object being passed in does not have any chance of failing (i.e. you constructed
// the object yourself)
func MustMarshal(i interface{}) []byte {
	bytes, err := json.Marshal(i)
	if err != nil {
		panic(err)
	}
	return bytes
}
