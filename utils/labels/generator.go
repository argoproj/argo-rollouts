package labels

type Label[K, V ~string] struct {
	Key   K
	Value V
}

func NewLabel[K ~string, V ~string](key K, value V) Label[K, V] {
	return Label[K, V]{
		Key:   key,
		Value: value,
	}
}

func (l Label[K, V]) ToMap() map[K]V {
	return map[K]V{
		l.Key: l.Value,
	}
}

func MergeLabels[K, V ~string](labels []Label[K, V]) map[string]string {
	labelMap := make(map[string]string)
	for _, label := range labels {
		labelMap[string(label.Key)] = string(label.Value)
	}
	return labelMap
}
