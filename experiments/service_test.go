package experiments

import (
	"testing"
)

func TestCreatedServiceHasExpectedAnnotations(t *testing.T) {
	templates := generateTemplates("bar", "baz")
	e := newExperiment("foo", templates, "")

	f := newFixture(t, e)
	defer f.Close()
}
