package rollout

import (
	"github.com/argoproj/pkg/grpc/http"
)

func init() {
	forward_RolloutService_WatchRolloutInfos_0 = http.StreamForwarder
	forward_RolloutService_WatchRolloutInfo_0 = http.StreamForwarder
}
