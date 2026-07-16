package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/argoproj/pkg/errors"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	log "github.com/sirupsen/logrus"
	"github.com/soheilhy/cmux"
	"google.golang.org/grpc"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	appslisters "k8s.io/client-go/listers/apps/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	"github.com/argoproj/argo-rollouts/pkg/apiclient/rollout"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	rolloutclientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	rolloutinformers "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions"
	listers "github.com/argoproj/argo-rollouts/pkg/client/listers/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/abort"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/get"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/promote"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/restart"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/retry"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/set"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/undo"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/info"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/viewcontroller"
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
}

const (
	// MaxGRPCMessageSize contains max grpc message size
	MaxGRPCMessageSize = 100 * 1024 * 1024
)

// ArgoRolloutsServer holds information about rollouts server
type ArgoRolloutsServer struct {
	Options ServerOptions
	stopCh  chan struct{}

	// Lazily-initialized, per-namespace informer-backed listers for the
	// one-shot list paths (ListRolloutInfos / ListReplicaSetsAndPods).
	// Without this, every page load issued unfiltered, full-payload List
	// calls for ALL ReplicaSets and Pods in the namespace, which takes the
	// control plane seconds to minutes on large namespaces.
	nsListersMu sync.Mutex
	nsListers   map[string]*namespaceListers
}

// namespaceListers serves reads from a shared watch cache instead of
// re-listing the world on every request, and fans informer events out to
// stream subscribers (WatchRolloutInfos) so streams don't build and sync
// their own informers per connection.
type namespaceListers struct {
	rollouts    listers.RolloutNamespaceLister
	replicaSets appslisters.ReplicaSetNamespaceLister
	pods        corelisters.PodNamespaceLister

	subsMu    sync.Mutex
	subs      map[uint64]chan *v1alpha1.Rollout
	nextSubID uint64
}

// subscribe registers a stream for rollout-change notifications. Channels
// are buffered and broadcast never blocks (see broadcast).
func (c *namespaceListers) subscribe() (uint64, <-chan *v1alpha1.Rollout) {
	c.subsMu.Lock()
	defer c.subsMu.Unlock()
	id := c.nextSubID
	c.nextSubID++
	ch := make(chan *v1alpha1.Rollout, 256)
	c.subs[id] = ch
	return id, ch
}

func (c *namespaceListers) unsubscribe(id uint64) {
	c.subsMu.Lock()
	defer c.subsMu.Unlock()
	delete(c.subs, id)
}

// broadcast delivers a changed rollout to every subscriber, dropping the
// event for any subscriber whose buffer is full — a dropped update is
// superseded by the next one, and the shared informer must never block on
// a slow stream.
func (c *namespaceListers) broadcast(ro *v1alpha1.Rollout) {
	c.subsMu.Lock()
	defer c.subsMu.Unlock()
	for _, ch := range c.subs {
		select {
		case ch <- ro:
		default:
		}
	}
}

// namespaceListers returns (building on first use) the informer-backed
// listers for a namespace. Informers are started once and live for the
// process lifetime — a warm watch is the point of the cache, and the
// dashboard serves a small, fixed set of namespaces.
func (s *ArgoRolloutsServer) namespaceListers(ctx context.Context, namespace string) (*namespaceListers, error) {
	s.nsListersMu.Lock()
	defer s.nsListersMu.Unlock()
	if c, ok := s.nsListers[namespace]; ok {
		return c, nil
	}

	stopCh := make(chan struct{})

	rolloutsFactory := rolloutinformers.NewSharedInformerFactoryWithOptions(
		s.Options.RolloutsClientset, 0, rolloutinformers.WithNamespace(namespace))
	rolloutInformer := rolloutsFactory.Argoproj().V1alpha1().Rollouts()

	// Only ReplicaSets/Pods managed by Argo Rollouts carry
	// DefaultRolloutUniqueLabelKey, and those are the only objects the
	// ownerRef walk in GetReplicaSetInfo can ever match — so let the API
	// server filter out everything else instead of shipping it to us.
	kubeFactory := kubeinformers.NewSharedInformerFactoryWithOptions(
		s.Options.KubeClientset, 0,
		kubeinformers.WithNamespace(namespace),
		kubeinformers.WithTweakListOptions(func(opts *v1.ListOptions) {
			opts.LabelSelector = v1alpha1.DefaultRolloutUniqueLabelKey
		}))
	rsInformer := kubeFactory.Apps().V1().ReplicaSets()
	podInformer := kubeFactory.Core().V1().Pods()

	// Informer() must be called BEFORE factory.Start — Start only launches
	// informers that are already registered with the factory.
	rolloutSynced := rolloutInformer.Informer().HasSynced
	rsSynced := rsInformer.Informer().HasSynced
	podSynced := podInformer.Informer().HasSynced

	rolloutsFactory.Start(stopCh)
	kubeFactory.Start(stopCh)

	// Bound the initial sync independently of the request context (the gRPC
	// context may carry no deadline, which would block forever on failure).
	syncCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if !cache.WaitForCacheSync(syncCtx.Done(), rolloutSynced, rsSynced, podSynced) {
		close(stopCh)
		return nil, fmt.Errorf("timed out waiting for informer caches to sync for namespace %q", namespace)
	}

	c := &namespaceListers{
		rollouts:    rolloutInformer.Lister().Rollouts(namespace),
		replicaSets: rsInformer.Lister().ReplicaSets(namespace),
		pods:        podInformer.Lister().Pods(namespace),
		subs:        make(map[uint64]chan *v1alpha1.Rollout),
	}

	// One set of event handlers on the SHARED informers feeds every stream
	// subscriber (this client-go can't remove handlers, so per-stream
	// handlers would leak). podUpdated maps pod deletions back to the owning
	// rollout; it sends on a channel, so bridge it to broadcast.
	podEvents := make(chan *v1alpha1.Rollout, 64)
	go func() {
		for ro := range podEvents {
			c.broadcast(ro)
		}
	}()
	rolloutInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			if ro, ok := obj.(*v1alpha1.Rollout); ok {
				c.broadcast(ro)
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			if ro, ok := newObj.(*v1alpha1.Rollout); ok {
				c.broadcast(ro)
			}
		},
	})
	podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: func(obj interface{}) {
			if pod, ok := obj.(*corev1.Pod); ok {
				podUpdated(pod, c.replicaSets, c.rollouts, podEvents)
			}
		},
	})

	if s.nsListers == nil {
		s.nsListers = make(map[string]*namespaceListers)
	}
	s.nsListers[namespace] = c
	return c, nil
}

// NewServer creates an ArgoRolloutsServer
func NewServer(o ServerOptions) *ArgoRolloutsServer {
	return &ArgoRolloutsServer{Options: o}
}

func (s *ArgoRolloutsServer) newHTTPServer(ctx context.Context, port int) *http.Server {
	mux := http.NewServeMux()
	endpoint := fmt.Sprintf("0.0.0.0:%d", port)

	httpS := http.Server{
		Addr:    endpoint,
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

	err := rollout.RegisterRolloutServiceHandlerFromEndpoint(ctx, gwmux, endpoint, opts)
	if err != nil {
		panic(err)
	}

	var apiHandler http.Handler = gwmux
	mux.Handle("/api/", apiHandler)
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
		conn, realErr = net.Listen("tcp", fmt.Sprintf(":%d", port))
		if realErr != nil {
			log.Warnf("failed listen: %v", realErr)
			return false, nil
		}
		return true, nil
	})
	errors.CheckError(realErr)

	startupMessage := fmt.Sprintf("Argo Rollouts api-server serving on port %d (namespace: %s)", port, s.Options.Namespace)
	if dashboard {
		startupMessage = fmt.Sprintf("Argo Rollouts Dashboard is now available at http://localhost:%d/%s", port, s.Options.RootPath)
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
	controller := s.initRolloutViewController(namespace, name, context.Background())
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
		rolloutUpdates <- roInfo
	})

	go get.Watch(ctx.Done(), rolloutUpdates, func(i *rollout.RolloutInfo) {
		ws.Send(i)
	})
	controller.Run(ctx)
	close(rolloutUpdates)
	return nil
}

func (s *ArgoRolloutsServer) ListReplicaSetsAndPods(ctx context.Context, namespace string) ([]*appsv1.ReplicaSet, []*corev1.Pod, error) {
	c, err := s.namespaceListers(ctx, namespace)
	if err != nil {
		return nil, nil, err
	}

	allReplicaSets, err := c.replicaSets.List(labels.Everything())
	if err != nil {
		return nil, nil, err
	}

	allPods, err := c.pods.List(labels.Everything())
	if err != nil {
		return nil, nil, err
	}

	return allReplicaSets, allPods, nil
}

// ListRolloutInfos returns a list of all rollouts
func (s *ArgoRolloutsServer) ListRolloutInfos(ctx context.Context, q *rollout.RolloutInfoListQuery) (*rollout.RolloutInfoList, error) {
	c, err := s.namespaceListers(ctx, q.GetNamespace())
	if err != nil {
		return nil, err
	}

	rollouts, err := c.rollouts.List(labels.Everything())
	if err != nil {
		return nil, err
	}
	// Listers return objects in undefined order; the direct API List this
	// replaces was name-ordered, so sort to keep the UI stable.
	sort.Slice(rollouts, func(i, j int) bool { return rollouts[i].Name < rollouts[j].Name })

	allReplicaSets, allPods, err := s.ListReplicaSetsAndPods(ctx, q.GetNamespace())
	if err != nil {
		return nil, err
	}

	var riList []*rollout.RolloutInfo
	for _, cur := range rollouts {
		ri := info.NewRolloutInfo(cur, nil, nil, nil, nil, nil)
		ri.ReplicaSets = info.GetReplicaSetInfo(cur.UID, cur, allReplicaSets, allPods)
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
	send := func(r *rollout.RolloutInfo) {
		err := ws.Send(&rollout.RolloutWatchEvent{
			Type:        "Updated",
			RolloutInfo: r,
		})
		if err != nil {
			return
		}
	}
	ctx := ws.Context()

	// Streams share the namespace informer cache: opening the dashboard no
	// longer builds and syncs a fresh informer set per connection (which
	// took 10-15s of full-namespace listing on large namespaces).
	c, err := s.namespaceListers(ctx, q.GetNamespace())
	if err != nil {
		return err
	}

	emit := func(ro *v1alpha1.Rollout) error {
		allPods, err := c.pods.List(labels.Everything())
		if err != nil {
			return err
		}
		allReplicaSets, err := c.replicaSets.List(labels.Everything())
		if err != nil {
			return err
		}
		// get shallow rollout info
		send(info.NewRolloutInfo(ro, allReplicaSets, allPods, nil, nil, nil))
		return nil
	}

	// Subscribe before snapshotting so nothing lands between the two (a
	// duplicate update is harmless).
	id, updates := c.subscribe()
	defer c.unsubscribe(id)

	// Initial snapshot from the warm cache — replaces the ADD-event flood
	// the per-stream informer sync used to produce.
	rollouts, err := c.rollouts.List(labels.Everything())
	if err != nil {
		return err
	}
	sort.Slice(rollouts, func(i, j int) bool { return rollouts[i].Name < rollouts[j].Name })
	for _, ro := range rollouts {
		if err := emit(ro); err != nil {
			return err
		}
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case ro := <-updates:
			if err := emit(ro); err != nil {
				return err
			}
		}
	}
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

func (s *ArgoRolloutsServer) getRollout(namespace string, name string) (*v1alpha1.Rollout, error) {
	rolloutsInformerFactory := rolloutinformers.NewSharedInformerFactoryWithOptions(s.Options.RolloutsClientset, 0, rolloutinformers.WithNamespace(namespace))
	cache.WaitForCacheSync(s.stopCh, rolloutsInformerFactory.Argoproj().V1alpha1().Rollouts().Informer().HasSynced)
	rolloutsLister := rolloutsInformerFactory.Argoproj().V1alpha1().Rollouts().Lister().Rollouts(namespace)
	return rolloutsLister.Get(name)
}

func (s *ArgoRolloutsServer) SetRolloutImage(ctx context.Context, q *rollout.SetImageRequest) (*v1alpha1.Rollout, error) {
	imageString := fmt.Sprintf("%s:%s", q.GetImage(), q.GetTag())
	_, err := set.SetImage(s.Options.DynamicClientset, q.GetNamespace(), q.GetRollout(), q.GetContainer(), imageString)
	if err != nil {
		return nil, err
	}
	return s.getRollout(q.GetNamespace(), q.GetRollout())
}

func (s *ArgoRolloutsServer) UndoRollout(ctx context.Context, q *rollout.UndoRolloutRequest) (*v1alpha1.Rollout, error) {
	rolloutIf := s.Options.DynamicClientset.Resource(v1alpha1.RolloutGVR).Namespace(q.GetNamespace())
	_, err := undo.RunUndoRollout(rolloutIf, s.Options.KubeClientset, q.GetRollout(), q.GetRevision())
	if err != nil {
		return nil, err
	}
	return s.getRollout(q.GetNamespace(), q.GetRollout())
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

func podUpdated(pod *corev1.Pod, rsLister appslisters.ReplicaSetNamespaceLister,
	rolloutLister listers.RolloutNamespaceLister, rolloutUpdated chan *v1alpha1.Rollout) {
	for _, podOwner := range pod.GetOwnerReferences() {
		if podOwner.Kind == "ReplicaSet" {
			rs, err := rsLister.Get(podOwner.Name)
			if err == nil {
				for _, rsOwner := range rs.GetOwnerReferences() {
					if rsOwner.APIVersion == v1alpha1.SchemeGroupVersion.String() && rsOwner.Kind == "Rollout" {
						ro, err := rolloutLister.Get(rsOwner.Name)
						if err == nil {
							rolloutUpdated <- ro
						}
					}
				}
			}
		}
	}
}
