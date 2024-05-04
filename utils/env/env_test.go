package env

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseNumFromEnv(t *testing.T) {
	const envKey = "SOMEKEY"
	const min = math.MinInt + 1
	const max = math.MaxInt - 1
	const def = 10
	testCases := []struct {
		name     string
		env      string
		expected int
	}{
		{"Valid positive number", "200", 200},
		{"Valid negative number", "-200", -200},
		{"Invalid number", "abc", def},
		{"Equals minimum", fmt.Sprintf("%d", math.MinInt+1), min},
		{"Equals maximum", fmt.Sprintf("%d", math.MaxInt-1), max},
		{"Less than minimum", fmt.Sprintf("%d", math.MinInt), def},
		{"Greater than maximum", fmt.Sprintf("%d", math.MaxInt), def},
		{"Variable not set", "", def},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(envKey, tt.env)
			n := ParseNumFromEnv(envKey, def, min, max)
			assert.Equal(t, tt.expected, n)
		})
	}
}

func Test_ParseBoolFromEnv(t *testing.T) {
	envKey := "SOMEKEY"

	testCases := []struct {
		name     string
		env      string
		expected bool
		def      bool
	}{
		{"True value", "true", true, false},
		{"False value", "false", false, true},
		{"Invalid value with true default", "somevalue", true, true},
		{"Invalid value with false default", "somevalue", false, false},
		{"Env not set", "", false, false},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(envKey, tt.env)
			b := ParseBoolFromEnv(envKey, tt.def)
			assert.Equal(t, tt.expected, b)
		})
	}
}
