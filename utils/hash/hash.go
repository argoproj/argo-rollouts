package hash

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/fnv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/rand"
)

// ComputePodTemplateHash returns a hash value calculated from pod template.
// The hash will be safe encoded to avoid bad words.
func ComputePodTemplateHash(template *corev1.PodTemplateSpec, collisionCount *int32) string {
	podTemplateSpecHasher := fnv.New32a()
	stepsBytes, err := json.Marshal(template)
	if err != nil {
		panic(err)
	}
	stepsBytes = restoreOmittedCreationTimestamp(template, stepsBytes)
	_, err = podTemplateSpecHasher.Write(stepsBytes)
	if err != nil {
		panic(err)
	}
	if collisionCount != nil {
		collisionCountBytes := make([]byte, 8)
		binary.LittleEndian.PutUint32(collisionCountBytes, uint32(*collisionCount))
		_, err = podTemplateSpecHasher.Write(collisionCountBytes)
		if err != nil {
			panic(err)
		}
	}
	return rand.SafeEncodeString(fmt.Sprint(podTemplateSpecHasher.Sum32()))
}

// restoreOmittedCreationTimestamp keeps the pod-template-hash stable across
// Kubernetes library upgrades.
//
// Starting with k8s.io/apimachinery v0.31, metav1.ObjectMeta.CreationTimestamp
// gained the `omitzero` JSON tag. Combined with Go 1.24+, a zero timestamp is
// now omitted from the marshaled output entirely, whereas it previously
// serialized as `"creationTimestamp":null`. Because the hash is an FNV sum over
// the marshaled bytes, this silently changes the hash for every Rollout on
// upgrade, triggering a spurious new revision that progresses through the
// canary steps. We re-insert the field so the marshaled bytes (and therefore
// the hash) match what older controller versions produced.
//
// The pod template embeds exactly one ObjectMeta (rendered as the single
// "metadata" object; PodSpec has no nested ObjectMeta), and CreationTimestamp
// is the first ObjectMeta field that is serialized for a typical template, so a
// single replacement at the start of the metadata object is correct.
func restoreOmittedCreationTimestamp(template *corev1.PodTemplateSpec, marshaled []byte) []byte {
	if !template.CreationTimestamp.IsZero() {
		return marshaled
	}
	if bytes.Contains(marshaled, []byte(`"creationTimestamp"`)) {
		return marshaled
	}
	if bytes.Contains(marshaled, []byte(`"metadata":{}`)) {
		return bytes.Replace(marshaled, []byte(`"metadata":{}`), []byte(`"metadata":{"creationTimestamp":null}`), 1)
	}
	if bytes.Contains(marshaled, []byte(`"metadata":{`)) {
		return bytes.Replace(marshaled, []byte(`"metadata":{`), []byte(`"metadata":{"creationTimestamp":null,`), 1)
	}
	return marshaled
}
