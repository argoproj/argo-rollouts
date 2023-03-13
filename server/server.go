package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
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
	rolloutIf := s.Options.RolloutsClientset.ArgoprojV1alpha1().Rollouts(q.GetNamespace())
	rolloutList, err := rolloutIf.List(ctx, v1.ListOptions{})

	if err != nil {
		return nil, err
	}

	allReplicaSets, allPods, err := s.ListReplicaSetsAndPods(ctx, q.GetNamespace())
	if err != nil {
		return nil, err
	}

	var riList []*rollout.RolloutInfo
	for i := range rolloutList.Items {
		cur := rolloutList.Items[i]
		ri := info.NewRolloutInfo(&cur, nil, nil, nil, nil, nil)
		ri.ReplicaSets = info.GetReplicaSetInfo(cur.UID, &cur, allReplicaSets, allPods)
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

	rolloutsInformerFactory := rolloutinformers.NewSharedInformerFactoryWithOptions(s.Options.RolloutsClientset, 0, rolloutinformers.WithNamespace(q.Namespace))
	rolloutsLister := rolloutsInformerFactory.Argoproj().V1alpha1().Rollouts().Lister().Rollouts(q.Namespace)
	rolloutInformer := rolloutsInformerFactory.Argoproj().V1alpha1().Rollouts().Informer()

	kubeInformerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(s.Options.KubeClientset, 0, kubeinformers.WithNamespace(q.Namespace))
	podsLister := kubeInformerFactory.Core().V1().Pods().Lister().Pods(q.GetNamespace())
	rsLister := kubeInformerFactory.Apps().V1().ReplicaSets().Lister().ReplicaSets(q.GetNamespace())
	kubeInformerFactory.Start(ws.Context().Done())
	podsInformer := kubeInformerFactory.Core().V1().Pods().Informer()

	rolloutUpdateChan := make(chan *v1alpha1.Rollout)

	rolloutInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			rolloutUpdateChan <- obj.(*v1alpha1.Rollout)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			rolloutUpdateChan <- newObj.(*v1alpha1.Rollout)
		},
	})
	podsInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: func(obj interface{}) {
			podUpdated(obj.(*corev1.Pod), rsLister, rolloutsLister, rolloutUpdateChan)
		},
	})

	go rolloutInformer.Run(ctx.Done())

	cache.WaitForCacheSync(
		ws.Context().Done(),
		podsInformer.HasSynced,
		kubeInformerFactory.Apps().V1().ReplicaSets().Informer().HasSynced,
		rolloutInformer.HasSynced,
	)

	for {
		select {
		case <-ctx.Done():
			return nil
		case ro := <-rolloutUpdateChan:
			allPods, err := podsLister.List(labels.Everything())
			if err != nil {
				return err
			}
			allReplicaSets, err := rsLister.List(labels.Everything())
			if err != nil {
				return err
			}

			// get shallow rollout info
			ri := info.NewRolloutInfo(ro, allReplicaSets, allPods, nil, nil, nil)
			send(ri)
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
