package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/argoproj/argo-rollouts/pkg/apiclient/rollout"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/info"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/viewcontroller"
)

type ArgoRolloutsServer struct {
}

// Run starts the server
func (s *ArgoRolloutsServer) Run(ctx context.Context, port int) {
	var httpS *http.Server
	httpS = &http.Server{
		Addr: addr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			target := "https://" + req.Host
			target += req.URL.Path
			if len(req.URL.RawQuery) > 0 {
				target += "?" + req.URL.RawQuery
			}
			http.Redirect(w, req, target, http.StatusTemporaryRedirect)
		}),
	}

	// Start listener
	var conn net.Listener
	var realErr error
	_ = wait.ExponentialBackoff(backoff, func() (bool, error) {
		conn, realErr = net.Listen("tcp", fmt.Sprintf(":%d", port))
		if realErr != nil {
			a.log.Warnf("failed listen: %v", realErr)
			return false, nil
		}
		return true, nil
	})
	errors.CheckError(realErr)

	log.Infof("argo-rollouts %s serving on port %d (url: %s, tls: false, namespace: %s)",
		common.GetVersion(), port, s.settings.URL, s.Namespace)

	go func () {
		httpS.Serve(httpL)
	}

	s.stopCh = make(chan struct{})
	<-s.stopCh
	errors.CheckError(conn.Close())
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