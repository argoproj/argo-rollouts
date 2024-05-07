package diff

import (
	"encoding/json"

	"k8s.io/apimachinery/pkg/util/strategicpatch"
)

// CreateTwoWayMergePatch is a helper to construct a two-way merge patch from objects (instead of bytes)
func CreateTwoWayMergePatch(orig, new, dataStruct any) ([]byte, bool, error) {
	origBytes, err := json.Marshal(orig)
	if err != nil {
		return nil, false, err
	}
	newBytes, err := json.Marshal(new)
	if err != nil {
		return nil, false, err
	}
	patch, err := strategicpatch.CreateTwoWayMergePatch(origBytes, newBytes, dataStruct)
	if err != nil {
		return nil, false, err
	}
	return patch, string(patch) != "{}", nil
}

// CreateThreeWayMergePatch is a helper to construct a two-way merge patch from objects (instead of bytes)
func CreateThreeWayMergePatch(orig, new, current any) ([]byte, bool, error) {
	origBytes, err := json.Marshal(orig)
	if err != nil {
		return nil, false, err
	}
	newBytes, err := json.Marshal(new)
	if err != nil {
		return nil, false, err
	}
	currentBytes, err := json.Marshal(current)
	if err != nil {
		return nil, false, err
	}

	lookupPatchMeta, err := strategicpatch.NewPatchMetaFromStruct(current)
	if err != nil {
		return nil, false, err
	}
	patch, err := strategicpatch.CreateThreeWayMergePatch(origBytes, newBytes, currentBytes, lookupPatchMeta, false)
	if err != nil {
		return nil, false, err
	}

	return patch, string(patch) != "{}", nil
}
