package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	log "github.com/sirupsen/logrus"
	"github.com/soheilhy/cmux"
	"google.golang.org/grpc"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"github.com/argoproj/argo-rollouts/pkg/apiclient/rollout"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	rolloutclientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/abort"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/promote"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/restart"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/retry"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/set"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/undo"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/info"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/viewcontroller"
	"github.com/argoproj/argo-rollouts/utils/errors"
	"github.com/argoproj/argo-rollouts/utils/json"
	versionutils "github.com/argoproj/argo-rollouts/utils/version"
)

var backoff = wait.Backoff{
	Steps:    5,
	Duration: 500 * time.Millisecond,
	Factor:   1.0,
	Jitter:   0.1,
}

type ServerOptions struct {
	KubeClientset     kubernetes.Interface
	RolloutsClientset rolloutclientset.Interface
	DynamicClientset  dynamic.Interface
	Namespace         string
	RootPath          string
	// ListenAddr is the address the dashboard/API server listens on. Defaults to
	// localhost-only (defaultListenAddr) when empty, since the dashboard command is
	// typically run on a user's workstation.
	ListenAddr string
}

const (
	// MaxGRPCMessageSize contains max grpc message size
	MaxGRPCMessageSize = 100 * 1024 * 1024
)

// ArgoRolloutsServer holds information about rollouts server
type ArgoRolloutsServer struct {
	Options ServerOptions
	stopCh  chan struct{}

	informers *informerCache
}

// NewServer creates an ArgoRolloutsServer
func NewServer(o ServerOptions) *ArgoRolloutsServer {
	return &ArgoRolloutsServer{
		Options:   o,
		informers: newInformerCache(o.KubeClientset, o.RolloutsClientset),
	}
}

const (
	defaultListenAddr = "127.0.0.1"

	// rolloutInfoMinSendInterval is the minimum time between two updates sent on a
	// WatchRolloutInfo stream.
	rolloutInfoMinSendInterval = 200 * time.Millisecond

	// rolloutInfosCoalesceInterval is how long a WatchRolloutInfos stream collects
	// changed Rollouts before sending them, so that a burst of events for the same
	// Rollout results in a single update.
	rolloutInfosCoalesceInterval = 250 * time.Millisecond
)

func (s *ArgoRolloutsServer) listenAddr() string {
	if s.Options.ListenAddr != "" {
		return s.Options.ListenAddr
	}
	return defaultListenAddr
}

// connectAddr returns the address the grpc-gateway should connect to, to reach the
// in-process gRPC server.
func (s *ArgoRolloutsServer) connectAddr() string {
	switch addr := s.listenAddr(); addr {
	case "0.0.0.0", "::", "":
		return "localhost"
	default:
		return addr
	}
}

func (s *ArgoRolloutsServer) newHTTPServer(ctx context.Context, port int) *http.Server {
	mux := http.NewServeMux()

	httpS := http.Server{
		Addr:    net.JoinHostPort(s.listenAddr(), fmt.Sprintf("%d", port)),
		Handler: mux,
	}

	gwMuxOpts := runtime.WithMarshalerOption(runtime.MIMEWildcard, new(json.JSONMarshaler))
	gwmux := runtime.NewServeMux(gwMuxOpts,
		runtime.WithIncomingHeaderMatcher(func(key string) (string, bool) {
			// Dropping "Connection" header as a workaround for https://github.com/grpc-ecosystem/grpc-gateway/issues/2447
			// The fix is part of grpc-gateway v2.x but not available in v1.x, so workaround should be removed after upgrading to grpc v2.x
			return key, strings.ToLower(key) != "connection"
		}),
		runtime.WithProtoErrorHandler(runtime.DefaultHTTPProtoErrorHandler),
	)

	opts := []grpc.DialOption{
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(MaxGRPCMessageSize)),
	}
	opts = append(opts, grpc.WithInsecure())

	endpoint := net.JoinHostPort(s.connectAddr(), fmt.Sprintf("%d", port))
	err := rollout.RegisterRolloutServiceHandlerFromEndpoint(ctx, gwmux, endpoint, opts)
	if err != nil {
		panic(err)
	}

	var apiHandler http.Handler = gwmux

	// Mount API at rootPath/api/ when rootPath is configured
	apiPath := "/api/"
	if s.Options.RootPath != "" {
		apiPath = path.Join("/", s.Options.RootPath, "api") + "/"
		stripPrefix := path.Join("/", s.Options.RootPath)
		apiHandler = http.StripPrefix(stripPrefix, gwmux)
	}
	mux.Handle(apiPath, apiHandler)
	mux.HandleFunc("/", s.staticFileHttpHandler)

	return &httpS
}

func (s *ArgoRolloutsServer) newGRPCServer() *grpc.Server {
	grpcS := grpc.NewServer()
	var rolloutsServer rollout.RolloutServiceServer = NewServer(s.Options)
	rollout.RegisterRolloutServiceServer(grpcS, rolloutsServer)
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
		conn, realErr = net.Listen("tcp", net.JoinHostPort(s.listenAddr(), fmt.Sprintf("%d", port)))
		if realErr != nil {
			log.Warnf("failed listen: %v", realErr)
			return false, nil
		}
		return true, nil
	})
	errors.CheckError(realErr)

	startupMessage := fmt.Sprintf("Argo Rollouts api-server serving on port %s %d (namespace: %s)", s.listenAddr(), port, s.Options.Namespace)
	if dashboard {
		startupMessage = fmt.Sprintf("Argo Rollouts Dashboard is now available at http://%s:%d/%s", s.listenAddr(), port, s.Options.RootPath)
	}

	log.Info(startupMessage)

	tcpm := cmux.New(conn)

	httpL := tcpm.Match(cmux.HTTP1Fast())
	grpcL := tcpm.Match(cmux.Any())

	go func() {
		s.checkServeErr("httpServer", httpServer.Serve(httpL))
	}()
	go func() {
		s.checkServeErr("grpcServer", grpcServer.Serve(grpcL))
	}()
	go func() { s.checkServeErr("tcpm", tcpm.Serve()) }()

	s.stopCh = make(chan struct{})
	<-s.stopCh
	errors.CheckError(conn.Close())
}

func (s *ArgoRolloutsServer) initRolloutViewController(namespace string, name string, ctx context.Context) *viewcontroller.RolloutViewController {
	controller := viewcontroller.NewRolloutViewController(namespace, name, s.Options.KubeClientset, s.Options.RolloutsClientset)
	controller.Start(ctx)
	return controller
}

func (s *ArgoRolloutsServer) getRolloutInfo(namespace string, name string) (*rollout.RolloutInfo, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	controller := s.initRolloutViewController(namespace, name, ctx)
	ri, err := controller.GetRolloutInfo()
	if err != nil {
		return nil, err
	}
	return ri, nil
}

// GetRolloutInfo returns a rollout
func (s *ArgoRolloutsServer) GetRolloutInfo(c context.Context, q *rollout.RolloutInfoQuery) (*rollout.RolloutInfo, error) {
	return s.getRolloutInfo(q.GetNamespace(), q.GetName())
}

// WatchRolloutInfo returns a rollout stream
func (s *ArgoRolloutsServer) WatchRolloutInfo(q *rollout.RolloutInfoQuery, ws rollout.RolloutService_WatchRolloutInfoServer) error {
	ctx := ws.Context()
	controller := s.initRolloutViewController(q.GetNamespace(), q.GetName(), ctx)

	rolloutUpdates := make(chan *rollout.RolloutInfo)
	controller.RegisterCallback(func(roInfo *rollout.RolloutInfo) {
		select {
		case rolloutUpdates <- roInfo:
		case <-ctx.Done():
		}
	})

	go sendRolloutInfoUpdates(ctx, rolloutUpdates, func(i *rollout.RolloutInfo) {
		ws.Send(i)
	})
	controller.Run(ctx)
	return nil
}

// sendRolloutInfoUpdates sends the latest RolloutInfo received on updates, at most once
// per rolloutInfoMinSendInterval. Nothing is sent while there are no updates.
func sendRolloutInfoUpdates(ctx context.Context, updates <-chan *rollout.RolloutInfo, send func(*rollout.RolloutInfo)) {
	var pending *rollout.RolloutInfo
	var lastSent time.Time
	var timer <-chan time.Time
	flush := func() {
		send(pending)
		pending = nil
		timer = nil
		lastSent = time.Now()
	}
	for {
		select {
		case <-ctx.Done():
			return
		case pending = <-updates:
			if wait := rolloutInfoMinSendInterval - time.Since(lastSent); wait <= 0 {
				flush()
			} else if timer == nil {
				timer = time.After(wait)
			}
		case <-timer:
			flush()
		}
	}
}

func (s *ArgoRolloutsServer) ListReplicaSetsAndPods(ctx context.Context, namespace string) ([]*appsv1.ReplicaSet, []*corev1.Pod, error) {

	allReplicaSets, err := s.Options.KubeClientset.AppsV1().ReplicaSets(namespace).List(ctx, v1.ListOptions{})
	if err != nil {
		return nil, nil, err
	}

	allPods, err := s.Options.KubeClientset.CoreV1().Pods(namespace).List(ctx, v1.ListOptions{})
	if err != nil {
		return nil, nil, err
	}

	var allReplicaSetsP = make([]*appsv1.ReplicaSet, len(allReplicaSets.Items))
	for i := range allReplicaSets.Items {
		allReplicaSetsP[i] = &allReplicaSets.Items[i]
	}
	var allPodsP = make([]*corev1.Pod, len(allPods.Items))
	for i := range allPods.Items {
		allPodsP[i] = &allPods.Items[i]
	}
	return allReplicaSetsP, allPodsP, nil
}

// ListRolloutInfos returns a list of all rollouts
func (s *ArgoRolloutsServer) ListRolloutInfos(ctx context.Context, q *rollout.RolloutInfoListQuery) (*rollout.RolloutInfoList, error) {
	nc, release, err := s.informers.acquire(ctx, q.GetNamespace())
	if err != nil {
		return nil, err
	}
	defer release()

	var riList []*rollout.RolloutInfo
	for _, ro := range nc.rollouts() {
		ri, err := nc.rolloutInfo(ro)
		if err != nil {
			return nil, err
		}
		riList = append(riList, ri)
	}

	return &rollout.RolloutInfoList{Rollouts: riList}, nil
}

func (s *ArgoRolloutsServer) RestartRollout(ctx context.Context, q *rollout.RestartRolloutRequest) (*v1alpha1.Rollout, error) {
	rolloutIf := s.Options.RolloutsClientset.ArgoprojV1alpha1().Rollouts(q.GetNamespace())
	restartAt := time.Now().UTC()
	return restart.RestartRollout(rolloutIf, q.GetName(), &restartAt)
}

// WatchRolloutInfos returns a stream of all rollouts
func (s *ArgoRolloutsServer) WatchRolloutInfos(q *rollout.RolloutInfoListQuery, ws rollout.RolloutService_WatchRolloutInfosServer) error {
	ctx := ws.Context()

	nc, release, err := s.informers.acquire(ctx, q.GetNamespace())
	if err != nil {
		return err
	}
	defer release()

	changed := newKeySet()
	enqueueRollout := func(obj any) {
		if key, err := cache.MetaNamespaceKeyFunc(obj); err == nil {
			changed.add(key)
		}
	}
	rolloutRegistration, err := nc.rolloutInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: enqueueRollout,
		UpdateFunc: func(oldObj, newObj any) {
			enqueueRollout(newObj)
		},
	})
	if err != nil {
		return err
	}
	defer func() { _ = nc.rolloutInformer.RemoveEventHandler(rolloutRegistration) }()
	podRegistration, err := nc.podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: func(obj any) {
			if pod, ok := obj.(*corev1.Pod); ok {
				changed.add(nc.rolloutKeysForPod(pod)...)
			}
		},
	})
	if err != nil {
		return err
	}
	defer func() { _ = nc.podInformer.RemoveEventHandler(podRegistration) }()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-changed.notify:
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(rolloutInfosCoalesceInterval):
		}

		for _, key := range changed.drain() {
			ro, err := nc.rollout(key)
			if err != nil {
				return err
			}
			if ro == nil {
				continue
			}
			// get shallow rollout info
			ri, err := nc.rolloutInfo(ro)
			if err != nil {
				return err
			}
			err = ws.Send(&rollout.RolloutWatchEvent{
				Type:        "Updated",
				RolloutInfo: ri,
			})
			if err != nil {
				return err
			}
		}
	}
}

// keySet collects the keys of changed objects until they are drained.
type keySet struct {
	mu   sync.Mutex
	keys map[string]struct{}
	// notify receives a value when keys were added since the last drain
	notify chan struct{}
}

func newKeySet() *keySet {
	return &keySet{
		keys:   map[string]struct{}{},
		notify: make(chan struct{}, 1),
	}
}

func (k *keySet) add(keys ...string) {
	if len(keys) == 0 {
		return
	}
	k.mu.Lock()
	for _, key := range keys {
		k.keys[key] = struct{}{}
	}
	k.mu.Unlock()
	select {
	case k.notify <- struct{}{}:
	default:
	}
}

func (k *keySet) drain() []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	keys := make([]string, 0, len(k.keys))
	for key := range k.keys {
		keys = append(keys, key)
	}
	k.keys = map[string]struct{}{}
	sort.Strings(keys)
	return keys
}

func (s *ArgoRolloutsServer) RolloutToRolloutInfo(ro *v1alpha1.Rollout) (*rollout.RolloutInfo, error) {
	ctx := context.Background()
	allReplicaSets, allPods, err := s.ListReplicaSetsAndPods(ctx, ro.Namespace)
	if err != nil {
		return nil, err
	}
	return info.NewRolloutInfo(ro, allReplicaSets, allPods, nil, nil, nil), nil
}

func (s *ArgoRolloutsServer) GetNamespace(ctx context.Context, e *empty.Empty) (*rollout.NamespaceInfo, error) {
	var m = make(map[string]bool)
	var namespaces []string

	rolloutList, err := s.Options.RolloutsClientset.ArgoprojV1alpha1().Rollouts("").List(ctx, v1.ListOptions{})
	if err == nil {
		for _, r := range rolloutList.Items {
			ns := r.Namespace
			if !m[ns] {
				m[ns] = true
				namespaces = append(namespaces, ns)
			}
		}
	}

	return &rollout.NamespaceInfo{Namespace: s.Options.Namespace, AvailableNamespaces: namespaces}, nil
}

func (s *ArgoRolloutsServer) PromoteRollout(ctx context.Context, q *rollout.PromoteRolloutRequest) (*v1alpha1.Rollout, error) {
	rolloutIf := s.Options.RolloutsClientset.ArgoprojV1alpha1().Rollouts(q.GetNamespace())
	return promote.PromoteRollout(rolloutIf, q.GetName(), false, false, q.GetFull())
}

func (s *ArgoRolloutsServer) AbortRollout(ctx context.Context, q *rollout.AbortRolloutRequest) (*v1alpha1.Rollout, error) {
	rolloutIf := s.Options.RolloutsClientset.ArgoprojV1alpha1().Rollouts(q.GetNamespace())
	return abort.AbortRollout(rolloutIf, q.GetName())
}

func (s *ArgoRolloutsServer) getRollout(ctx context.Context, namespace string, name string) (*v1alpha1.Rollout, error) {
	return s.Options.RolloutsClientset.ArgoprojV1alpha1().Rollouts(namespace).Get(ctx, name, v1.GetOptions{})
}

func (s *ArgoRolloutsServer) SetRolloutImage(ctx context.Context, q *rollout.SetImageRequest) (*v1alpha1.Rollout, error) {
	imageString := fmt.Sprintf("%s:%s", q.GetImage(), q.GetTag())
	_, err := set.SetImage(s.Options.DynamicClientset, q.GetNamespace(), q.GetRollout(), q.GetContainer(), imageString)
	if err != nil {
		return nil, err
	}
	return s.getRollout(ctx, q.GetNamespace(), q.GetRollout())
}

func (s *ArgoRolloutsServer) UndoRollout(ctx context.Context, q *rollout.UndoRolloutRequest) (*v1alpha1.Rollout, error) {
	rolloutIf := s.Options.DynamicClientset.Resource(v1alpha1.RolloutGVR).Namespace(q.GetNamespace())
	_, err := undo.RunUndoRollout(rolloutIf, s.Options.KubeClientset, q.GetRollout(), q.GetRevision())
	if err != nil {
		return nil, err
	}
	return s.getRollout(ctx, q.GetNamespace(), q.GetRollout())
}

func (s *ArgoRolloutsServer) RetryRollout(ctx context.Context, q *rollout.RetryRolloutRequest) (*v1alpha1.Rollout, error) {
	rolloutIf := s.Options.RolloutsClientset.ArgoprojV1alpha1().Rollouts(q.GetNamespace())
	ro, err := retry.RetryRollout(rolloutIf, q.GetName())
	if err != nil {
		return nil, err
	}

	return ro, nil
}

func (s *ArgoRolloutsServer) Version(ctx context.Context, _ *empty.Empty) (*rollout.VersionInfo, error) {
	version := versionutils.GetVersion()
	return &rollout.VersionInfo{
		RolloutsVersion: version.String(),
	}, nil
}
