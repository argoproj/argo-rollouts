package env

import (
	"math"
	"os"
	"strconv"
	"strings"
)

// ParseBoolFromEnv retrieves a boolean value from given environment envVar.
// Returns default value if envVar is not set.
func ParseBoolFromEnv(envVar string, defaultValue bool) bool {
	if val := os.Getenv(envVar); val != "" {
		if strings.ToLower(val) == "true" {
			return true
		} else if strings.ToLower(val) == "false" {
			return false
		}
	}
	return defaultValue
}

// ParseNumFromEnv Helper function to parse a number from an environment variable. Returns a
// default if env is not set, is not parseable to a number, exceeds max (if
// max is greater than 0) or is less than min.
func ParseNumFromEnv(env string, defaultValue, min, max int) int {
	str := os.Getenv(env)
	if str == "" {
		return defaultValue
	}
	num, err := strconv.ParseInt(str, 10, 0)
	if err != nil {
		return defaultValue
	}
	if num > math.MaxInt || num < math.MinInt {
		return defaultValue
	}
	if int(num) < min {
		return defaultValue
	}
	if int(num) > max {
		return defaultValue
	}
	return int(num)
}
