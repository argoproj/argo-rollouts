package rollout

import (
	"github.com/argoproj/pkg/grpc/http"
)

func init() {
	forward_RolloutService_WatchRollouts_0 = http.StreamForwarder
	forward_RolloutService_WatchRollout_0 = http.StreamForwarder
}
