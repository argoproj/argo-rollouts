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
	rolloutclientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/get"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/info"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/viewcontroller"
	"github.com/argoproj/argo-rollouts/utils/json"
	"github.com/argoproj/pkg/errors"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	log "github.com/sirupsen/logrus"
	"github.com/soheilhy/cmux"
	"google.golang.org/grpc"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

var backoff = wait.Backoff{
	Steps:    5,
	Duration: 500 * time.Millisecond,
	Factor:   1.0,
	Jitter:   0.1,
}
type ServerOptions struct {
	KubeClientset kubernetes.Interface
	RolloutsClientset rolloutclientset.Interface
	Namespace string
}

const (
	// MaxGRPCMessageSize contains max grpc message size
	MaxGRPCMessageSize = 100 * 1024 * 1024
)

// ArgoRolloutsServer holds information about rollouts server
type ArgoRolloutsServer struct {
	Options ServerOptions
	stopCh chan struct{}
}

// NewServer creates an ArgoRolloutsServer
func NewServer(o ServerOptions) *ArgoRolloutsServer {
	return &ArgoRolloutsServer{Options: o};
}

func (s* ArgoRolloutsServer) newHTTPServer(ctx context.Context, port int) *http.Server {
	mux := http.NewServeMux()
	endpoint := fmt.Sprintf("0.0.0.0:%d", port)

	httpS := http.Server{
		Addr: endpoint,
		Handler: mux,
	}

	gwMuxOpts := runtime.WithMarshalerOption(runtime.MIMEWildcard, new(json.JSONMarshaler))
	gwmux := runtime.NewServeMux(gwMuxOpts,
		runtime.WithIncomingHeaderMatcher(func(key string) (string, bool) { return key, true }),
		runtime.WithProtoErrorHandler(runtime.DefaultHTTPProtoErrorHandler),
	)

	opts := []grpc.DialOption{
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(MaxGRPCMessageSize)),
	}
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
	var rolloutsServer rolloutspkg.RolloutServiceServer = NewServer(s.Options)
	rolloutspkg.RegisterRolloutServiceServer(grpcS, rolloutsServer)
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

	log.Infof("Argo Rollouts api-server serving on port %d (namespace: %s)", port, s.Options.Namespace)

	tcpm := cmux.New(conn)

	httpL := tcpm.Match(cmux.HTTP1Fast())
	grpcL := tcpm.Match(cmux.Any())
	
	go func () {
		s.checkServeErr("httpServer", httpServer.Serve(httpL))
	}()
	go func () {
		s.checkServeErr("grpcServer", grpcServer.Serve(grpcL))
	}()
	go func() { s.checkServeErr("tcpm", tcpm.Serve()) }()

	s.stopCh = make(chan struct{})
	<-s.stopCh
	errors.CheckError(conn.Close())
}

func (s* ArgoRolloutsServer) initRolloutViewController(name string, ctx context.Context) *viewcontroller.RolloutViewController {
	controller := viewcontroller.NewRolloutViewController(s.Options.Namespace, name, s.Options.KubeClientset, s.Options.RolloutsClientset)
	controller.Start(ctx)
	return controller
}

func infoToResponse(ri *info.RolloutInfo) *v1alpha1.RolloutInfo {
	return &v1alpha1.RolloutInfo{
		ObjectMeta: v1.ObjectMeta{ Name: ri.Metadata.Name, Namespace: ri.Metadata.Namespace},
		Status: ri.Status,
	}
}

// Get returns a rollout
func (s* ArgoRolloutsServer) Get(c context.Context, q *rollout.RolloutQuery) (*v1alpha1.RolloutInfo, error) {
	controller := s.initRolloutViewController(q.GetName(), context.Background())
	ri, err := controller.GetRolloutInfo()
	if (err != nil) {
		return nil, err
	}
	return infoToResponse(ri), nil
}

// Watch returns a rollout
func (s* ArgoRolloutsServer) Watch(q *rollout.RolloutQuery, ws rollout.RolloutService_WatchServer) error {
	ctx := context.Background()
	controller := s.initRolloutViewController(q.GetName(), ctx)

	rolloutUpdates := make(chan *info.RolloutInfo)
	controller.RegisterCallback(func(roInfo *info.RolloutInfo) {
		rolloutUpdates <- roInfo
	})
	
	go get.Watch(ctx.Done(), rolloutUpdates, func(i *info.RolloutInfo) {
		ws.Send(infoToResponse(i))
	})
	controller.Run(ctx)
	close(rolloutUpdates)
	return nil
}