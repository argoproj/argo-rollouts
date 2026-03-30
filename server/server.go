package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"path"
	"strings"
	"time"

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
	"k8s.io/client-go/rest"
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
	AuthMode          string
	OIDCConfig        OIDCConfig
	// RESTConfig is the base Kubernetes REST configuration (without auth credentials).
	// Used to create per-request clients with user tokens for RBAC enforcement.
	RESTConfig *rest.Config
}

const (
	// MaxGRPCMessageSize contains max grpc message size
	MaxGRPCMessageSize = 100 * 1024 * 1024
)

// serverClients holds Kubernetes client interfaces used for API operations
type serverClients struct {
	kubeClientset     kubernetes.Interface
	rolloutsClientset rolloutclientset.Interface
	dynamicClientset  dynamic.Interface
}

// ArgoRolloutsServer holds information about rollouts server
type ArgoRolloutsServer struct {
	Options ServerOptions
	stopCh  chan struct{}
}

// NewServer creates an ArgoRolloutsServer
func NewServer(o ServerOptions) *ArgoRolloutsServer {
	return &ArgoRolloutsServer{Options: o}
}

// getClients returns Kubernetes clients for the current request.
// In token auth mode, it creates per-request clients using the user's bearer token
// from the gRPC context, ensuring Kubernetes RBAC is enforced per user.
// In server mode, it returns the server's shared clients.
func (s *ArgoRolloutsServer) getClients(ctx context.Context) (*serverClients, error) {
	if s.Options.AuthMode != AuthModeToken || s.Options.RESTConfig == nil {
		return &serverClients{
			kubeClientset:     s.Options.KubeClientset,
			rolloutsClientset: s.Options.RolloutsClientset,
			dynamicClientset:  s.Options.DynamicClientset,
		}, nil
	}

	token := tokenFromGRPCContext(ctx)
	if token == "" {
		return &serverClients{
			kubeClientset:     s.Options.KubeClientset,
			rolloutsClientset: s.Options.RolloutsClientset,
			dynamicClientset:  s.Options.DynamicClientset,
		}, nil
	}

	return s.clientsFromToken(token)
}

// clientsFromToken creates Kubernetes clients authenticated with the given bearer token
func (s *ArgoRolloutsServer) clientsFromToken(token string) (*serverClients, error) {
	cfg := rest.CopyConfig(s.Options.RESTConfig)
	cfg.BearerToken = token
	cfg.BearerTokenFile = ""
	cfg.Username = ""
	cfg.Password = ""
	cfg.CertData = nil
	cfg.CertFile = ""
	cfg.KeyData = nil
	cfg.KeyFile = ""

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create kube client from token: %w", err)
	}

	rolloutsClient, err := rolloutclientset.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create rollouts client from token: %w", err)
	}

	dynamicClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client from token: %w", err)
	}

	return &serverClients{
		kubeClientset:     kubeClient,
		rolloutsClientset: rolloutsClient,
		dynamicClientset:  dynamicClient,
	}, nil
}

const (
	listenAddr  = "0.0.0.0"
	connectAddr = "localhost"
)

func (s *ArgoRolloutsServer) newHTTPServer(ctx context.Context, port int) *http.Server {
	mux := http.NewServeMux()

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

	endpoint := net.JoinHostPort(connectAddr, fmt.Sprintf("%d", port))
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

	// Register OIDC routes when configured
	if s.Options.OIDCConfig.IsConfigured() {
		oidcHandler, err := newOIDCHandler(s.Options.OIDCConfig, s.Options.RootPath)
		if err != nil {
			log.Fatalf("Failed to initialize OIDC: %v", err)
		}
		oidcHandler.registerOIDCRoutes(mux, s.Options.RootPath)
	}

	mux.HandleFunc("/", s.staticFileHttpHandler)

	// Wrap with auth middleware when token auth mode is enabled
	var handler http.Handler = mux
	if s.Options.AuthMode == AuthModeToken {
		authenticator := newTokenAuthenticator(s.Options.KubeClientset)
		authMiddleware := newTokenAuthMiddleware(authenticator)
		handler = authMiddleware(mux)
		log.Info("Token authentication enabled for dashboard")
	}

	httpS := http.Server{
		Addr:    net.JoinHostPort(listenAddr, fmt.Sprintf("%d", port)),
		Handler: handler,
	}

	return &httpS
}

func (s *ArgoRolloutsServer) newGRPCServer() *grpc.Server {
	var grpcOpts []grpc.ServerOption
	if s.Options.AuthMode == AuthModeToken {
		authenticator := newTokenAuthenticator(s.Options.KubeClientset)
		grpcOpts = append(grpcOpts,
			grpc.UnaryInterceptor(newAuthUnaryInterceptor(authenticator)),
			grpc.StreamInterceptor(newAuthStreamInterceptor(authenticator)),
		)
	}
	grpcS := grpc.NewServer(grpcOpts...)
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
	if s.Options.AuthMode == AuthModeToken {
		startupMessage += " (authentication: token)"
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

func (s *ArgoRolloutsServer) initRolloutViewController(namespace string, name string, ctx context.Context, clients *serverClients) *viewcontroller.RolloutViewController {
	controller := viewcontroller.NewRolloutViewController(namespace, name, clients.kubeClientset, clients.rolloutsClientset)
	controller.Start(ctx)
	return controller
}

func (s *ArgoRolloutsServer) getRolloutInfo(ctx context.Context, namespace string, name string) (*rollout.RolloutInfo, error) {
	clients, err := s.getClients(ctx)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	controller := s.initRolloutViewController(namespace, name, ctx, clients)
	ri, err := controller.GetRolloutInfo()
	if err != nil {
		return nil, err
	}
	return ri, nil
}

// GetRolloutInfo returns a rollout
func (s *ArgoRolloutsServer) GetRolloutInfo(c context.Context, q *rollout.RolloutInfoQuery) (*rollout.RolloutInfo, error) {
	return s.getRolloutInfo(c, q.GetNamespace(), q.GetName())
}

// WatchRolloutInfo returns a rollout stream
func (s *ArgoRolloutsServer) WatchRolloutInfo(q *rollout.RolloutInfoQuery, ws rollout.RolloutService_WatchRolloutInfoServer) error {
	ctx := ws.Context()
	clients, err := s.getClients(ctx)
	if err != nil {
		return err
	}
	controller := s.initRolloutViewController(q.GetNamespace(), q.GetName(), ctx, clients)

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

func (s *ArgoRolloutsServer) listReplicaSetsAndPods(ctx context.Context, namespace string, clients *serverClients) ([]*appsv1.ReplicaSet, []*corev1.Pod, error) {
	allReplicaSets, err := clients.kubeClientset.AppsV1().ReplicaSets(namespace).List(ctx, v1.ListOptions{})
	if err != nil {
		return nil, nil, err
	}

	allPods, err := clients.kubeClientset.CoreV1().Pods(namespace).List(ctx, v1.ListOptions{})
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

// ListReplicaSetsAndPods returns all ReplicaSets and Pods in a namespace using server clients
func (s *ArgoRolloutsServer) ListReplicaSetsAndPods(ctx context.Context, namespace string) ([]*appsv1.ReplicaSet, []*corev1.Pod, error) {
	clients, err := s.getClients(ctx)
	if err != nil {
		return nil, nil, err
	}
	return s.listReplicaSetsAndPods(ctx, namespace, clients)
}

// ListRolloutInfos returns a list of all rollouts
func (s *ArgoRolloutsServer) ListRolloutInfos(ctx context.Context, q *rollout.RolloutInfoListQuery) (*rollout.RolloutInfoList, error) {
	clients, err := s.getClients(ctx)
	if err != nil {
		return nil, err
	}

	rolloutIf := clients.rolloutsClientset.ArgoprojV1alpha1().Rollouts(q.GetNamespace())
	rolloutList, err := rolloutIf.List(ctx, v1.ListOptions{})
	if err != nil {
		return nil, err
	}

	allReplicaSets, allPods, err := s.listReplicaSetsAndPods(ctx, q.GetNamespace(), clients)
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
	clients, err := s.getClients(ctx)
	if err != nil {
		return nil, err
	}
	rolloutIf := clients.rolloutsClientset.ArgoprojV1alpha1().Rollouts(q.GetNamespace())
	restartAt := time.Now().UTC()
	return restart.RestartRollout(rolloutIf, q.GetName(), &restartAt)
}

// WatchRolloutInfos returns a stream of all rollouts
func (s *ArgoRolloutsServer) WatchRolloutInfos(q *rollout.RolloutInfoListQuery, ws rollout.RolloutService_WatchRolloutInfosServer) error {
	ctx := ws.Context()
	clients, err := s.getClients(ctx)
	if err != nil {
		return err
	}

	send := func(r *rollout.RolloutInfo) {
		err := ws.Send(&rollout.RolloutWatchEvent{
			Type:        "Updated",
			RolloutInfo: r,
		})
		if err != nil {
			return
		}
	}

	rolloutsInformerFactory := rolloutinformers.NewSharedInformerFactoryWithOptions(clients.rolloutsClientset, 0, rolloutinformers.WithNamespace(q.Namespace))
	rolloutsLister := rolloutsInformerFactory.Argoproj().V1alpha1().Rollouts().Lister().Rollouts(q.Namespace)
	rolloutInformer := rolloutsInformerFactory.Argoproj().V1alpha1().Rollouts().Informer()

	kubeInformerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(clients.kubeClientset, 0, kubeinformers.WithNamespace(q.Namespace))
	podsLister := kubeInformerFactory.Core().V1().Pods().Lister().Pods(q.GetNamespace())
	rsLister := kubeInformerFactory.Apps().V1().ReplicaSets().Lister().ReplicaSets(q.GetNamespace())
	kubeInformerFactory.Start(ctx.Done())
	podsInformer := kubeInformerFactory.Core().V1().Pods().Informer()

	rolloutUpdateChan := make(chan *v1alpha1.Rollout)

	rolloutInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			rolloutUpdateChan <- obj.(*v1alpha1.Rollout)
		},
		UpdateFunc: func(oldObj, newObj any) {
			rolloutUpdateChan <- newObj.(*v1alpha1.Rollout)
		},
	})
	podsInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: func(obj any) {
			podUpdated(obj.(*corev1.Pod), rsLister, rolloutsLister, rolloutUpdateChan)
		},
	})

	go rolloutInformer.Run(ctx.Done())

	cache.WaitForCacheSync(
		ctx.Done(),
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
	clients, err := s.getClients(ctx)
	if err != nil {
		return nil, err
	}

	var m = make(map[string]bool)
	var namespaces []string

	rolloutList, err := clients.rolloutsClientset.ArgoprojV1alpha1().Rollouts("").List(ctx, v1.ListOptions{})
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
	clients, err := s.getClients(ctx)
	if err != nil {
		return nil, err
	}
	rolloutIf := clients.rolloutsClientset.ArgoprojV1alpha1().Rollouts(q.GetNamespace())
	return promote.PromoteRollout(rolloutIf, q.GetName(), false, false, q.GetFull())
}

func (s *ArgoRolloutsServer) AbortRollout(ctx context.Context, q *rollout.AbortRolloutRequest) (*v1alpha1.Rollout, error) {
	clients, err := s.getClients(ctx)
	if err != nil {
		return nil, err
	}
	rolloutIf := clients.rolloutsClientset.ArgoprojV1alpha1().Rollouts(q.GetNamespace())
	return abort.AbortRollout(rolloutIf, q.GetName())
}

func (s *ArgoRolloutsServer) getRollout(ctx context.Context, namespace string, name string) (*v1alpha1.Rollout, error) {
	clients, err := s.getClients(ctx)
	if err != nil {
		return nil, err
	}
	rolloutsInformerFactory := rolloutinformers.NewSharedInformerFactoryWithOptions(clients.rolloutsClientset, 0, rolloutinformers.WithNamespace(namespace))
	cache.WaitForCacheSync(s.stopCh, rolloutsInformerFactory.Argoproj().V1alpha1().Rollouts().Informer().HasSynced)
	rolloutsLister := rolloutsInformerFactory.Argoproj().V1alpha1().Rollouts().Lister().Rollouts(namespace)
	return rolloutsLister.Get(name)
}

func (s *ArgoRolloutsServer) SetRolloutImage(ctx context.Context, q *rollout.SetImageRequest) (*v1alpha1.Rollout, error) {
	clients, err := s.getClients(ctx)
	if err != nil {
		return nil, err
	}
	imageString := fmt.Sprintf("%s:%s", q.GetImage(), q.GetTag())
	_, err = set.SetImage(clients.dynamicClientset, q.GetNamespace(), q.GetRollout(), q.GetContainer(), imageString)
	if err != nil {
		return nil, err
	}
	return s.getRollout(ctx, q.GetNamespace(), q.GetRollout())
}

func (s *ArgoRolloutsServer) UndoRollout(ctx context.Context, q *rollout.UndoRolloutRequest) (*v1alpha1.Rollout, error) {
	clients, err := s.getClients(ctx)
	if err != nil {
		return nil, err
	}
	rolloutIf := clients.dynamicClientset.Resource(v1alpha1.RolloutGVR).Namespace(q.GetNamespace())
	_, err = undo.RunUndoRollout(rolloutIf, clients.kubeClientset, q.GetRollout(), q.GetRevision())
	if err != nil {
		return nil, err
	}
	return s.getRollout(ctx, q.GetNamespace(), q.GetRollout())
}

func (s *ArgoRolloutsServer) RetryRollout(ctx context.Context, q *rollout.RetryRolloutRequest) (*v1alpha1.Rollout, error) {
	clients, err := s.getClients(ctx)
	if err != nil {
		return nil, err
	}
	rolloutIf := clients.rolloutsClientset.ArgoprojV1alpha1().Rollouts(q.GetNamespace())
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
