package trafficrouting

type WeightDestination struct {
	ServiceName     string
	PodTemplateHash string
	Weight          int32
}
