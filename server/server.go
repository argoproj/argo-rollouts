package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/argoproj/argo-rollouts/common"
	"github.com/argoproj/argo-rollouts/pkg/apiclient/rollout"
	settingspkg "github.com/argoproj/argo-rollouts/pkg/apiclient/settings"
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
	"github.com/argoproj/argo-rollouts/server/settings"
	errorsutil "github.com/argoproj/argo-rollouts/utils/errors"
	httputil "github.com/argoproj/argo-rollouts/utils/http"
	"github.com/argoproj/argo-rollouts/utils/json"
	jwtutil "github.com/argoproj/argo-rollouts/utils/jwt"
	"github.com/argoproj/argo-rollouts/utils/oidc"
	utils_session "github.com/argoproj/argo-rollouts/utils/session"
	settings_util "github.com/argoproj/argo-rollouts/utils/settings"
	versionutils "github.com/argoproj/argo-rollouts/utils/version"
	"github.com/argoproj/pkg/errors"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	log "github.com/sirupsen/logrus"
	"github.com/soheilhy/cmux"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
  "github.com/golang-jwt/jwt/v4"
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
  DisableAuth       bool
}

const (
	// MaxGRPCMessageSize contains max grpc message size
	MaxGRPCMessageSize = 100 * 1024 * 1024
  renewTokenKey = "renew-token"
)

// ArgoRolloutsServer holds information about rollouts server
type ArgoRolloutsServer struct {
	Options ServerOptions

  ssoClientApp   *oidc.ClientApp
  settingsMgr *settings_util.SettingsManager
  serviceSet  *ArgoRolloutsServiceSet
  settings    *settings_util.ArgoRolloutsSettings
  sessionMgr  *utils_session.SessionManager
	stopCh  chan struct{}
}

// NewServer creates an ArgoRolloutsServer
func NewServer(ctx context.Context, opts ServerOptions) *ArgoRolloutsServer {
  settingsMgr := settings_util.NewSettingsManager(ctx, opts.KubeClientset, opts.Namespace)
	settings, err := settingsMgr.InitializeSettings(true) //TODO implement insecure options
	errorsutil.CheckError(err)

	return &ArgoRolloutsServer{
    Options: opts,
    settingsMgr: settingsMgr,
    settings: settings,
  }
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

  errRegisterSettings := settingspkg.RegisterSettingsServiceHandlerFromEndpoint(ctx, gwmux, endpoint, opts)
  if errRegisterSettings != nil {
    panic(errRegisterSettings)
  }

	var apiHandler http.Handler = gwmux
	mux.Handle("/api/", apiHandler)
	mux.HandleFunc("/", s.staticFileHttpHandler)

	return &httpS
}

type ArgoRolloutsServiceSet struct {
	SettingsService       *settings.Server
  RolloutService        rollout.RolloutServiceServer
}

func newArgoRolloutsServiceSet(ctx context.Context, s *ArgoRolloutsServer) *ArgoRolloutsServiceSet {
	settingsService := settings.NewServer(s.settingsMgr, s, s.Options.DisableAuth)
  rolloutsServer := NewServer(ctx, s.Options)

	return &ArgoRolloutsServiceSet{
		SettingsService: settingsService,
    RolloutService:  rolloutsServer,
	}
}

func (s *ArgoRolloutsServer) newGRPCServer() *grpc.Server {
	grpcS := grpc.NewServer()

	rollout.RegisterRolloutServiceServer(grpcS, s.serviceSet.RolloutService)
  settingspkg.RegisterSettingsServiceServer(grpcS, s.serviceSet.SettingsService)

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
  svcSet := newArgoRolloutsServiceSet(ctx, s)
	s.serviceSet = svcSet
  
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

// Authenticate checks for the presence of a valid token when accessing server-side resources.
func (a *ArgoRolloutsServer) Authenticate(ctx context.Context) (context.Context, error) {
	if a.Options.DisableAuth {
		return ctx, nil
	}
	claims, newToken, claimsErr := a.getClaims(ctx)
	if claims != nil {
		// Add claims to the context to inspect for RBAC
		// nolint:staticcheck
		ctx = context.WithValue(ctx, "claims", claims)
		if newToken != "" {
			// Session tokens that are expiring soon should be regenerated if user stays active.
			// The renewed token is stored in outgoing ServerMetadata. Metadata is available to grpc-gateway
			// response forwarder that will translate it into Set-Cookie header.
			if err := grpc.SendHeader(ctx, metadata.New(map[string]string{renewTokenKey: newToken})); err != nil {
				log.Warnf("Failed to set %s header", renewTokenKey)
			}
		}
	}
	if claimsErr != nil {
		// nolint:staticcheck
		ctx = context.WithValue(ctx, utils_session.AuthErrorCtxKey, claimsErr)
	}

	if claimsErr != nil {
		argoCDSettings, err := a.settingsMgr.GetSettings()
		if err != nil {
			return ctx, status.Errorf(codes.Internal, "unable to load settings: %v", err)
		}
		if !argoCDSettings.AnonymousUserEnabled {
			return ctx, claimsErr
		} else {
			// nolint:staticcheck
			ctx = context.WithValue(ctx, "claims", "")
		}
	}

	return ctx, nil
}

// ErrNoSession indicates no auth token was supplied as part of a request
var ErrNoSession = status.Errorf(codes.Unauthenticated, "no session information")

func (a *ArgoRolloutsServer) getClaims(ctx context.Context) (jwt.Claims, string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, "", ErrNoSession
	}
	tokenString := getToken(md)
	if tokenString == "" {
		return nil, "", ErrNoSession
	}
	claims, newToken, err := a.sessionMgr.VerifyToken(tokenString)
	if err != nil {
		return claims, "", status.Errorf(codes.Unauthenticated, "invalid session: %v", err)
	}

	// Some SSO implementations (Okta) require a call to
	// the OIDC user info path to get attributes like groups
	// we assume that everywhere in argocd jwt.MapClaims is used as type for interface jwt.Claims
	// otherwise this would cause a panic
	var groupClaims jwt.MapClaims
	if groupClaims, ok = claims.(jwt.MapClaims); !ok {
		if tmpClaims, ok := claims.(*jwt.MapClaims); ok {
			groupClaims = *tmpClaims
		}
	}
	iss := jwtutil.StringField(groupClaims, "iss")
	if iss != utils_session.SessionManagerClaimsIssuer && a.settings.UserInfoGroupsEnabled() && a.settings.UserInfoPath() != "" {
		userInfo, unauthorized, err := a.ssoClientApp.GetUserInfo(groupClaims, a.settings.IssuerURL(), a.settings.UserInfoPath())
		if unauthorized {
			log.Errorf("error while quering userinfo endpoint: %v", err)
			return claims, "", status.Errorf(codes.Unauthenticated, "invalid session")
		}
		if err != nil {
			log.Errorf("error fetching user info endpoint: %v", err)
			return claims, "", status.Errorf(codes.Internal, "invalid userinfo response")
		}
		if groupClaims["sub"] != userInfo["sub"] {
			return claims, "", status.Error(codes.Unknown, "subject of claims from user info endpoint didn't match subject of idToken, see https://openid.net/specs/openid-connect-core-1_0.html#UserInfo")
		}
		groupClaims["groups"] = userInfo["groups"]
	}

	return groupClaims, newToken, nil
}

const (
	MetaDataTokenKey = "token"
)

// getToken extracts the token from gRPC metadata or cookie headers
func getToken(md metadata.MD) string {
	// check the "token" metadata
	{
		tokens, ok := md[MetaDataTokenKey]
		if ok && len(tokens) > 0 {
			return tokens[0]
		}
	}

	// looks for the HTTP header `Authorization: Bearer ...`
	// argocd prefers bearer token over cookie
	for _, t := range md["authorization"] {
		token := strings.TrimPrefix(t, "Bearer ")
		if strings.HasPrefix(t, "Bearer ") && jwtutil.IsValid(token) {
			return token
		}
	}

	// check the HTTP cookie
	for _, t := range md["grpcgateway-cookie"] {
		header := http.Header{}
		header.Add("Cookie", t)
		request := http.Request{Header: header}
		token, err := httputil.JoinCookies(common.AuthCookieName, request.Cookies())
		if err == nil && jwtutil.IsValid(token) {
			return token
		}
	}

	return ""
}
