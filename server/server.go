package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/argoproj/argo-rollouts/pkg/apiclient/rollout"
	rolloutspkg "github.com/argoproj/argo-rollouts/pkg/apiclient/rollout"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/info"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/viewcontroller"
	"github.com/argoproj/argo-rollouts/utils/json"
	"github.com/argoproj/pkg/errors"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	log "github.com/sirupsen/logrus"
	"github.com/soheilhy/cmux"
	"google.golang.org/grpc"
	"k8s.io/apimachinery/pkg/util/wait"
)

var backoff = wait.Backoff{
	Steps:    5,
	Duration: 500 * time.Millisecond,
	Factor:   1.0,
	Jitter:   0.1,
}

// ArgoRolloutsServer holds information about rollouts server
type ArgoRolloutsServer struct {
	Namespace string
	stopCh chan struct{}
}

// NewServer creates an ArgoRolloutsServer
func NewServer(namespace string) *ArgoRolloutsServer {
	return &ArgoRolloutsServer{Namespace: namespace};
}

// NewRolloutsServer creates a RolloutServiceServer
func NewRolloutsServer(namespace string) rolloutspkg.RolloutServiceServer {
	return &ArgoRolloutsServer{Namespace: namespace}
}

func (s* ArgoRolloutsServer) newHTTPServer(ctx context.Context, port int) *http.Server {
	mux := http.NewServeMux()
	endpoint := fmt.Sprintf("localhost:%d", port)

	httpS := http.Server{
		Addr: endpoint,
		Handler: mux,
	}

	gwMuxOpts := runtime.WithMarshalerOption(runtime.MIMEWildcard, new(json.JSONMarshaler))
	gwmux := runtime.NewServeMux(gwMuxOpts,
		runtime.WithIncomingHeaderMatcher(func(key string) (string, bool) { return key, true }),
		runtime.WithProtoErrorHandler(runtime.DefaultHTTPProtoErrorHandler),
	)

	var opts []grpc.DialOption
	opts = append(opts, grpc.WithInsecure())

	err := rolloutspkg.RegisterRolloutServiceHandlerFromEndpoint(ctx, gwmux, endpoint, opts)
	if err != nil {
		panic(err)
	}

	var handler http.Handler = gwmux
	mux.Handle("/api/", handler)	

	return &httpS
}

func (s* ArgoRolloutsServer) newGRPCServer() *grpc.Server {
	grpcS := grpc.NewServer()
	rolloutspkg.RegisterRolloutServiceServer(grpcS, NewRolloutsServer(s.Namespace))
	return grpcS
}

func (s *ArgoRolloutsServer) checkServeErr(name string, err error) {
	if err != nil {
		if s.stopCh == nil {
			log.Infof("graceful shutdown %s: %v", name, err)
		} else {
			log.Fatalf("%s: %v", name, err)
		}
	} else {
		log.Infof("graceful shutdown %s", name)
	}
}

// Run starts the server
func (s *ArgoRolloutsServer) Run(ctx context.Context, port int) {
	httpServer := s.newHTTPServer(ctx, port)
	grpcServer := s.newGRPCServer()

	// Start listener
	var conn net.Listener
	var realErr error
	_ = wait.ExponentialBackoff(backoff, func() (bool, error) {
		conn, realErr = net.Listen("tcp", fmt.Sprintf(":%d", port))
		if realErr != nil {
			log.Warnf("failed listen: %v", realErr)
			return false, nil
		}
		return true, nil
	})
	errors.CheckError(realErr)

	log.Infof("Argo Rollouts api-server serving on port %d (namespace: %s)", port, s.Namespace)

	tcpm := cmux.New(conn)

	httpL := tcpm.Match(cmux.HTTP1Fast())
	grpcL := tcpm.Match(cmux.HTTP2HeaderField("content-type", "application/grpc"))
	
	go func () {
		s.checkServeErr("httpServer", httpServer.Serve(httpL))
	}()
	go func () {
		s.checkServeErr("grpcServer", grpcServer.Serve(grpcL))
	}()

	s.stopCh = make(chan struct{})
	<-s.stopCh
	errors.CheckError(conn.Close())
}

// RolloutInfo returns info stream for requested rollout
func (s* ArgoRolloutsServer) WatchInfo(q *rollout.RolloutInfoRequest, ws rollout.RolloutService_WatchInfoServer) error {
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

func (s* ArgoRolloutsServer) Hello(c context.Context, e *empty.Empty) (*rollout.Greeting, error) {
	log.Info("Hello")
	return &rollout.Greeting{Text: "Hello!"}, nil
}