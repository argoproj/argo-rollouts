package server

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/argoproj/argo-rollouts/pkg/apiclient/rollout"
	rolloutspkg "github.com/argoproj/argo-rollouts/pkg/apiclient/rollout"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	rolloutclientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/abort"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/get"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/promote"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/restart"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/set"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/undo"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/info"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/viewcontroller"
	"github.com/argoproj/argo-rollouts/utils/json"
	"github.com/argoproj/pkg/errors"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	log "github.com/sirupsen/logrus"
	"github.com/soheilhy/cmux"
	"google.golang.org/grpc"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

//go:embed static/*
var static embed.FS

var (
	watchAPIBufferSize = 1000
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
	DynamicClientset dynamic.Interface
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

type spaFileSystem struct {
	root http.FileSystem
}

func (fs *spaFileSystem) Open(name string) (http.File, error) {
	f, err := fs.root.Open(name)
	if os.IsNotExist(err) {
		return fs.root.Open("index.html")
	}
	return f, err
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

	ui, err := fs.Sub(static, "static")
	if err != nil {
		log.Error("Could not load UI static files")
		panic(err)
	}

	mux.Handle("/api/", handler)
	mux.Handle("/", http.FileServer(&spaFileSystem{http.FS(ui)}))

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
func (s *ArgoRolloutsServer) Run(ctx context.Context, port int, dashboard bool) {
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

	startupMessage := fmt.Sprintf("Argo Rollouts api-server serving on port %d (namespace: %s)", port, s.Options.Namespace)
	if (dashboard == true) {
		startupMessage = fmt.Sprintf("Argo Rollouts Dashboard is now available at localhost %d", port)
	}

	log.Info(startupMessage)

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

func (s* ArgoRolloutsServer) getRolloutInfo(name string) (*v1alpha1.RolloutInfo, error) {
	controller := s.initRolloutViewController(name, context.Background())
	ri, err := controller.GetRolloutInfo()
	if (err != nil) {
		return nil, err
	}
	return ri, nil
}

// GetRollout returns a rollout
func (s* ArgoRolloutsServer) GetRollout(c context.Context, q *rollout.RolloutQuery) (*v1alpha1.RolloutInfo, error) {
	return s.getRolloutInfo(q.GetName());
}

// WatchRollout returns a rollout stream
func (s* ArgoRolloutsServer) WatchRollout(q *rollout.RolloutQuery, ws rollout.RolloutService_WatchRolloutServer) error {
	ctx := context.Background()
	controller := s.initRolloutViewController(q.GetName(), ctx)

	rolloutUpdates := make(chan *v1alpha1.RolloutInfo)
	controller.RegisterCallback(func(roInfo *v1alpha1.RolloutInfo) {
		rolloutUpdates <- roInfo
	})
	
	go get.Watch(ctx.Done(), rolloutUpdates, func(i *v1alpha1.RolloutInfo) {
		ws.Send(i)
	})
	controller.Run(ctx)
	close(rolloutUpdates)
	return nil
}

// ListRollouts returns a list of all rollouts
func (s* ArgoRolloutsServer) ListRollouts(ctx context.Context, e *empty.Empty) (*v1alpha1.RolloutList, error) {
	rolloutIf := s.Options.RolloutsClientset.ArgoprojV1alpha1().Rollouts(s.Options.Namespace)
	rolloutList, err := rolloutIf.List(ctx, v1.ListOptions{})
	if (err != nil) {
		return nil, err
	}
	return rolloutList, nil
}

func (s* ArgoRolloutsServer) RestartRollout(ctx context.Context, q *rollout.RolloutQuery) (*empty.Empty, error) {
	rolloutIf := s.Options.RolloutsClientset.ArgoprojV1alpha1().Rollouts(s.Options.Namespace)
	restartAt := time.Now().UTC()
	restart.RestartRollout(rolloutIf, q.GetName(), &restartAt)
	return &empty.Empty{}, nil
}

// WatchRollouts returns a stream of all rollouts
func (s* ArgoRolloutsServer) WatchRollouts(q *empty.Empty, ws rollout.RolloutService_WatchRolloutsServer) error {
	send := func(r* v1alpha1.RolloutInfo) {
		err := ws.Send(&rollout.RolloutWatchEvent{
			Type:        "Updated",
			RolloutInfo:     r,
		})
		if err != nil {
			return
		}
	}
	ctx := context.Background()
	rolloutIf := s.Options.RolloutsClientset.ArgoprojV1alpha1().Rollouts(s.Options.Namespace)
	rolloutList, err := rolloutIf.List(ctx, v1.ListOptions{})
	if err != nil {
		return err
	}

	for i := range(rolloutList.Items) {
		// only do intensive get for initial list
		ri, err := s.getRolloutInfo(rolloutList.Items[i].ObjectMeta.Name)
		if (err != nil) {
			return nil
		}
		err = ws.Send(&rollout.RolloutWatchEvent{
			Type:        "Added",
			RolloutInfo:     ri,
		})
		if err != nil {
			return err
		}
	}

	watchIf, err := rolloutIf.Watch(ctx, v1.ListOptions{})
	if err != nil {
		return err
	}

	var ro *v1alpha1.Rollout
	retries := 0
L:
	for {
		select {
		case next := <-watchIf.ResultChan():
			ro, _ = next.Object.(*v1alpha1.Rollout)
		case <-ctx.Done():
			break L
		}
		if ro == nil {
			watchIf.Stop()
			newWatchIf, err := rolloutIf.Watch(ctx, v1.ListOptions{})
			if err != nil {
				if retries > 5 {
					return err
				}
				log.Warn(err)
				// this sleep prevents a hot-loop in the event there is a persistent error
				time.Sleep(time.Second)
				retries++
			} else {
				watchIf = newWatchIf
				retries = 0
			}
			continue
		}
		// get shallow rollout info
		ri := info.NewRolloutInfo(ro, nil, nil, nil, nil)
		send(ri)
	}
	watchIf.Stop()
	return nil
}

func (s* ArgoRolloutsServer) GetNamespace(ctx context.Context, e* empty.Empty) (*rollout.NamespaceInfo, error) {
	return &rollout.NamespaceInfo{ Namespace: s.Options.Namespace }, nil
}

func (s* ArgoRolloutsServer) PromoteRollout(ctx context.Context, q *rollout.RolloutQuery) (*empty.Empty, error) {
	rolloutIf := s.Options.RolloutsClientset.ArgoprojV1alpha1().Rollouts(s.Options.Namespace)
	_, err := promote.PromoteRollout(rolloutIf, q.GetName(), false, false, false)
	if (err != nil) {
		return nil, err
	}
	return &empty.Empty{}, nil
}

func (s* ArgoRolloutsServer) AbortRollout(ctx context.Context, q *rollout.RolloutQuery) (*empty.Empty, error) {
	rolloutIf := s.Options.RolloutsClientset.ArgoprojV1alpha1().Rollouts(s.Options.Namespace)
	_, err := abort.AbortRollout(rolloutIf, q.GetName())
	if (err != nil) {
		return nil, err
	}
	return &empty.Empty{}, nil
}

func (s* ArgoRolloutsServer) SetRolloutImage(ctx context.Context, q *rollout.SetImageQuery) (*empty.Empty, error) {
	imageString := fmt.Sprintf("%s:%s", q.GetImage(), q.GetTag())
	set.SetImage(s.Options.DynamicClientset, s.Options.Namespace, q.GetRollout(), q.GetContainer(), imageString)	
	return &empty.Empty{}, nil
}

func (s* ArgoRolloutsServer) UndoRollout(ctx context.Context, q *rollout.UndoQuery) (*empty.Empty, error) {
	rolloutIf := s.Options.DynamicClientset.Resource(v1alpha1.RolloutGVR).Namespace(s.Options.Namespace)

	_, err := undo.RunUndoRollout(rolloutIf, s.Options.KubeClientset, q.GetRollout(), q.GetRevision())
	if err != nil {
		return nil, err
	}
	return &empty.Empty{}, nil
}