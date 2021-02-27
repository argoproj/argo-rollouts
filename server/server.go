package server

import (
	"context"
	"time"

	"github.com/argoproj/argo-rollouts/pkg/apiclient/rollout"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"

	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/info"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/viewcontroller"
)

type ArgoRolloutsServer struct {
	viewController viewcontroller.RolloutViewController
}

// RolloutInfo returns info stream for requested rollout
func (s* ArgoRolloutsServer) RolloutInfo(q *rollout.RolloutInfoRequest, ws rollout.RolloutService_WatchInfoServer) error {
	o := options.ArgoRolloutsOptions{}

	controller := viewcontroller.NewRolloutViewController(q.Namespace, q.Name, o.KubeClientset(), o.RolloutsClientset())
	ctx := context.Background()
	controller.Start(ctx)

	rolloutUpdates := make(chan *info.RolloutInfo)
	controller.RegisterCallback(func(roInfo *info.RolloutInfo) {
		rolloutUpdates <- roInfo
	})
	go func() {
		rolloutUpdates := make(chan *info.RolloutInfo)
		ticker := time.NewTicker(time.Second)
		var currRolloutInfo *info.RolloutInfo
		var preventFlicker time.Time

		for {
			select {
			case roInfo := <-rolloutUpdates:
				currRolloutInfo = roInfo
			case <-ticker.C:
			case <-rolloutUpdates:
				return
			}
			if currRolloutInfo != nil && time.Now().After(preventFlicker.Add(200*time.Millisecond)) {
				ws.Send(&v1alpha1.RolloutInfo{
					Status: currRolloutInfo.Status,
				});
				preventFlicker = time.Now()
			}
		}	
	}()
	controller.Run(ctx)
	close(rolloutUpdates)
	return nil
}