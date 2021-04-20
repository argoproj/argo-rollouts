package json

import (
	"encoding/json"
	"io"

	gwruntime "github.com/grpc-ecosystem/grpc-gateway/runtime"
)

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

// JSONMarshaler is a type which satisfies the grpc-gateway Marshaler interface
type JSONMarshaler struct{}

// ContentType implements gwruntime.Marshaler.
func (j *JSONMarshaler) ContentType() string {
	return "application/json"
}

// Marshal implements gwruntime.Marshaler.
func (j *JSONMarshaler) Marshal(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

// NewDecoder implements gwruntime.Marshaler.
func (j *JSONMarshaler) NewDecoder(r io.Reader) gwruntime.Decoder {
	return json.NewDecoder(r)
}

// NewEncoder implements gwruntime.Marshaler.
func (j *JSONMarshaler) NewEncoder(w io.Writer) gwruntime.Encoder {
	return json.NewEncoder(w)
}

// Unmarshal implements gwruntime.Marshaler.
func (j *JSONMarshaler) Unmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}
