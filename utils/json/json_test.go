package json

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

type TestStruct struct {
	Foo string `json:"foo"`
}

func TestMustMarshalSuccess(t *testing.T) {
	test := TestStruct{
		Foo: "hi",
	}
	bytes := MustMarshal(test)
	assert.Equal(t, `{"foo":"hi"}`, string(bytes))
}

type I interface {
	Foo() string
}

func TestMustMarshalPanics(t *testing.T) {
	test := map[string]interface{}{
		"foo": make(chan int),
	}
	assert.Panics(t, func() { MustMarshal(test) })
}
