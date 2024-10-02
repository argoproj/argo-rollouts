package settings

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"path"
	"reflect"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	apiv1 "k8s.io/api/core/v1"
	apierr "k8s.io/apimachinery/pkg/api/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	v1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	v1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/yaml"

	timeutil "github.com/argoproj/pkg/time"

	"github.com/argoproj/argo-rollouts/common"
	"github.com/argoproj/argo-rollouts/server/settings/oidc"
	"github.com/argoproj/argo-rollouts/utils/crypto"
	tlsutil "github.com/argoproj/argo-rollouts/utils/tls"
	"github.com/argoproj/argo-rollouts/utils/util"
)

// ArgoRolloutsSettings holds in-memory runtime configuration options.
type ArgoRolloutsSettings struct {
	// URL is the externally facing URL users will visit to reach Argo Rollouts.
	// The value here is used when configuring SSO. Omitting this value will disable SSO.
	URL string `json:"url,omitempty"`
	// DexConfig contains portions of a dex config yaml
	DexConfig string `json:"dexConfig,omitempty"`
	// OIDCConfigRAW holds OIDC configuration as a raw string
	OIDCConfigRAW string `json:"oidcConfig,omitempty"`
	// ServerSignature holds the key used to generate JWT tokens.
	ServerSignature []byte `json:"serverSignature,omitempty"`
	// Certificate holds the certificate/private key for the Argo Rollouts dashboard.
	// If nil, will run insecure without TLS.
	Certificate *tls.Certificate `json:"-"`
	// CertificateIsExternal indicates whether Certificate was loaded from external secret
	CertificateIsExternal bool `json:"-"`
	// Secrets holds all secrets in argo-rollouts-secret as a map[string]string
	Secrets map[string]string `json:"secrets,omitempty"`
	// Specifies token expiration duration
	UserSessionDuration time.Duration `json:"userSessionDuration,omitempty"`
	// InClusterEnabled indicates whether to allow in-cluster server address
	InClusterEnabled bool `json:"inClusterEnabled"`
	// ServerRBACLogEnforceEnable temporary var indicates whether rbac will be enforced on logs
	ServerRBACLogEnforceEnable bool `json:"serverRBACLogEnforceEnable"`
	// OIDCTLSInsecureSkipVerify determines whether certificate verification is skipped when verifying tokens with the
	// configured OIDC provider (either external or the bundled Dex instance). Setting this to `true` will cause JWT
	// token verification to pass despite the OIDC provider having an invalid certificate. Only set to `true` if you
	// understand the risks.
	OIDCTLSInsecureSkipVerify bool `json:"oidcTLSInsecureSkipVerify"`
  // Indicates if anonymous user is enabled or not
	AnonymousUserEnabled bool `json:"anonymousUserEnabled,omitempty"`
}

// oidcConfig is the same as the public OIDCConfig, except the public one excludes the AllowedAudiences and the
// SkipAudienceCheckWhenTokenHasNoAudience fields.
// AllowedAudiences should be accessed via ArgoRolloutsSettings.OAuth2AllowedAudiences.
// SkipAudienceCheckWhenTokenHasNoAudience should be accessed via ArgoRolloutsSettings.SkipAudienceCheckWhenTokenHasNoAudience.
type oidcConfig struct {
	OIDCConfig
	AllowedAudiences                        []string `json:"allowedAudiences,omitempty"`
	SkipAudienceCheckWhenTokenHasNoAudience *bool    `json:"skipAudienceCheckWhenTokenHasNoAudience,omitempty"`
}

func (o *oidcConfig) toExported() *OIDCConfig {
	if o == nil {
		return nil
	}
	return &OIDCConfig{
		Name:                     o.Name,
		Issuer:                   o.Issuer,
		ClientID:                 o.ClientID,
		ClientSecret:             o.ClientSecret,
		CLIClientID:              o.CLIClientID,
		UserInfoPath:             o.UserInfoPath,
		EnableUserInfoGroups:     o.EnableUserInfoGroups,
		UserInfoCacheExpiration:  o.UserInfoCacheExpiration,
		RequestedScopes:          o.RequestedScopes,
		RequestedIDTokenClaims:   o.RequestedIDTokenClaims,
		LogoutURL:                o.LogoutURL,
		RootCA:                   o.RootCA,
		EnablePKCEAuthentication: o.EnablePKCEAuthentication,
		DomainHint:               o.DomainHint,
	}
}

type OIDCConfig struct {
	Name                     string                 `json:"name,omitempty"`
	Issuer                   string                 `json:"issuer,omitempty"`
	ClientID                 string                 `json:"clientID,omitempty"`
	ClientSecret             string                 `json:"clientSecret,omitempty"`
	CLIClientID              string                 `json:"cliClientID,omitempty"`
	EnableUserInfoGroups     bool                   `json:"enableUserInfoGroups,omitempty"`
	UserInfoPath             string                 `json:"userInfoPath,omitempty"`
	UserInfoCacheExpiration  string                 `json:"userInfoCacheExpiration,omitempty"`
	RequestedScopes          []string               `json:"requestedScopes,omitempty"`
	RequestedIDTokenClaims   map[string]*oidc.Claim `json:"requestedIDTokenClaims,omitempty"`
	LogoutURL                string                 `json:"logoutURL,omitempty"`
	RootCA                   string                 `json:"rootCA,omitempty"`
	EnablePKCEAuthentication bool                   `json:"enablePKCEAuthentication,omitempty"`
	DomainHint               string                 `json:"domainHint,omitempty"`
}

const (
	// settingServerSignatureKey designates the key for a server secret key inside a Kubernetes secret.
	settingServerSignatureKey = "server.secretkey"
	// settingServerCertificate designates the key for the public cert used in TLS
	settingServerCertificate = "tls.crt"
	// settingServerPrivateKey designates the key for the private key used in TLS
	settingServerPrivateKey = "tls.key"
	// settingURLKey designates the key whereArgo Rollouts's external URL is set
	settingURLKey = "url"
	// settingDexConfigKey designates the key for the dex config
	settingDexConfigKey = "dex.config"
	// settingsOIDCConfigKey designates the key for OIDC config
	settingsOIDCConfigKey = "oidc.config"
	// userSessionDurationKey is the key which specifies token expiration duration
	userSessionDurationKey = "users.session.duration"
	// externalServerTLSSecretName defines the name of the external secret holding the server's TLS certificate
	externalServerTLSSecretName = "argorollouts-server-tls"
	// partOfArgoCDSelector holds label selector that should be applied to config maps and secrets used to manageArgo Rollouts
	partOfArgoCDSelector = "app.kubernetes.io/part-of=argo-rollouts"
	// inClusterEnabledKey is the key to configure whether to allow in-cluster server address
	inClusterEnabledKey = "cluster.inClusterEnabled"
	// oidcTLSInsecureSkipVerifyKey is the key to configure whether TLS cert verification is skipped for OIDC connections
	oidcTLSInsecureSkipVerifyKey = "oidc.tls.insecure.skip.verify"
)

var (
	ByClusterURLIndexer     = "byClusterURL"
	byClusterURLIndexerFunc = func(obj interface{}) ([]string, error) {
		s, ok := obj.(*apiv1.Secret)
		if !ok {
			return nil, nil
		}
		if s.Labels == nil || s.Labels[common.LabelKeySecretType] != common.LabelValueSecretTypeCluster {
			return nil, nil
		}
		if s.Data == nil {
			return nil, nil
		}
		if url, ok := s.Data["server"]; ok {
			return []string{strings.TrimRight(string(url), "/")}, nil
		}
		return nil, nil
	}
	ByClusterNameIndexer     = "byClusterName"
	byClusterNameIndexerFunc = func(obj interface{}) ([]string, error) {
		s, ok := obj.(*apiv1.Secret)
		if !ok {
			return nil, nil
		}
		if s.Labels == nil || s.Labels[common.LabelKeySecretType] != common.LabelValueSecretTypeCluster {
			return nil, nil
		}
		if s.Data == nil {
			return nil, nil
		}
		if name, ok := s.Data["name"]; ok {
			return []string{string(name)}, nil
		}
		return nil, nil
	}
)

// SettingsManager holds config info for a new manager with which to access Kubernetes ConfigMaps.
type SettingsManager struct {
	ctx             context.Context
	clientset       kubernetes.Interface
	secrets         v1listers.SecretLister
	secretsInformer cache.SharedIndexInformer
	configmaps      v1listers.ConfigMapLister
	namespace       string
	// subscribers is a list of subscribers to settings updates
	subscribers []chan<- *ArgoRolloutsSettings
	// mutex protects concurrency sensitive parts of settings manager: access to subscribers list and initialization flag
	mutex                 *sync.Mutex
	initContextCancel     func()
}

type incompleteSettingsError struct {
	message string
}

type IgnoreStatus string

func (e *incompleteSettingsError) Error() string {
	return e.message
}

func (mgr *SettingsManager) GetSecretsLister() (v1listers.SecretLister, error) {
	err := mgr.ensureSynced(false)
	if err != nil {
		return nil, err
	}
	return mgr.secrets, nil
}

func (mgr *SettingsManager) GetSecretsInformer() (cache.SharedIndexInformer, error) {
	err := mgr.ensureSynced(false)
	if err != nil {
		return nil, fmt.Errorf("error ensuring that the secrets manager is synced: %w", err)
	}
	return mgr.secretsInformer, nil
}

func (mgr *SettingsManager) updateSecret(callback func(*apiv1.Secret) error) error {
	err := mgr.ensureSynced(false)
	if err != nil {
		return err
	}
	argoRolloutsSecret, err := mgr.secrets.Secrets(mgr.namespace).Get(common.ArgoRolloutsSecretName)
	createSecret := false
	if err != nil {
		if !apierr.IsNotFound(err) {
			return err
		}
		argoRolloutsSecret = &apiv1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: common.ArgoRolloutsSecretName,
			},
			Data: make(map[string][]byte),
		}
		createSecret = true
	}
	if argoRolloutsSecret.Data == nil {
		argoRolloutsSecret.Data = make(map[string][]byte)
	}

	updatedSecret := argoRolloutsSecret.DeepCopy()
	err = callback(updatedSecret)
	if err != nil {
		return err
	}

	if !createSecret && reflect.DeepEqual(argoRolloutsSecret.Data, updatedSecret.Data) {
		return nil
	}

	if createSecret {
		_, err = mgr.clientset.CoreV1().Secrets(mgr.namespace).Create(context.Background(), updatedSecret, metav1.CreateOptions{})
	} else {
		_, err = mgr.clientset.CoreV1().Secrets(mgr.namespace).Update(context.Background(), updatedSecret, metav1.UpdateOptions{})
	}
	if err != nil {
		return err
	}

	return mgr.ResyncInformers()
}

func (mgr *SettingsManager) updateConfigMap(callback func(*apiv1.ConfigMap) error) error {
	argoRolloutsCM, err := mgr.getConfigMap()
	createCM := false
	if err != nil {
		if !apierr.IsNotFound(err) {
			return err
		}
		argoRolloutsCM = &apiv1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name: common.ArgoRolloutsConfigMapName,
			},
		}
		createCM = true
	}
	if argoRolloutsCM.Data == nil {
		argoRolloutsCM.Data = make(map[string]string)
	}
	beforeUpdate := argoRolloutsCM.DeepCopy()
	err = callback(argoRolloutsCM)
	if err != nil {
		return err
	}
	if reflect.DeepEqual(beforeUpdate.Data, argoRolloutsCM.Data) {
		return nil
	}

	if createCM {
		_, err = mgr.clientset.CoreV1().ConfigMaps(mgr.namespace).Create(context.Background(), argoRolloutsCM, metav1.CreateOptions{})
	} else {
		_, err = mgr.clientset.CoreV1().ConfigMaps(mgr.namespace).Update(context.Background(), argoRolloutsCM, metav1.UpdateOptions{})
	}

	if err != nil {
		return err
	}

	mgr.invalidateCache()

	return mgr.ResyncInformers()
}

func (mgr *SettingsManager) getConfigMap() (*apiv1.ConfigMap, error) {
	err := mgr.ensureSynced(false)
	if err != nil {
		return nil, err
	}
	argoRolloutsCM, err := mgr.configmaps.ConfigMaps(mgr.namespace).Get(common.ArgoRolloutsConfigMapName)
	if err != nil {
		return nil, err
	}
	if argoRolloutsCM.Data == nil {
		argoRolloutsCM.Data = make(map[string]string)
	}
	return argoRolloutsCM, err
}

// Returns the ConfigMap with the given name from the cluster.
// The ConfigMap must be labeled with "app.kubernetes.io/part-of: argo-rollouts" in
// order to be retrievable.
func (mgr *SettingsManager) GetConfigMapByName(configMapName string) (*apiv1.ConfigMap, error) {
	err := mgr.ensureSynced(false)
	if err != nil {
		return nil, err
	}
	configMap, err := mgr.configmaps.ConfigMaps(mgr.namespace).Get(configMapName)
	if err != nil {
		return nil, err
	}
	return configMap, err
}

// GetSettings retrieves settings from the ArgoCDConfigMap and secret.
func (mgr *SettingsManager) GetSettings() (*ArgoRolloutsSettings, error) {
	err := mgr.ensureSynced(false)
	if err != nil {
		return nil, err
	}
	argoRolloutsCM, err := mgr.configmaps.ConfigMaps(mgr.namespace).Get(common.ArgoRolloutsConfigMapName)
	if err != nil {
		return nil, fmt.Errorf("error retrieving argo-rollouts-cm: %w", err)
	}
	argoRolloutsSecret, err := mgr.secrets.Secrets(mgr.namespace).Get(common.ArgoRolloutsSecretName)
	if err != nil {
		return nil, fmt.Errorf("error retrieving argo-rollouts-secret: %w", err)
	}
	selector, err := labels.Parse(partOfArgoCDSelector)
	if err != nil {
		return nil, fmt.Errorf("error parsing Argo Rollouts selector %w", err)
	}
	secrets, err := mgr.secrets.Secrets(mgr.namespace).List(selector)
	if err != nil {
		return nil, err
	}
	var settings ArgoRolloutsSettings
	var errs []error
	updateSettingsFromConfigMap(&settings, argoRolloutsCM)
	if err := mgr.updateSettingsFromSecret(&settings, argoRolloutsSecret, secrets); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return &settings, errs[0]
	}

	return &settings, nil
}

// Clears cached settings on configmap/secret change
func (mgr *SettingsManager) invalidateCache() {
	mgr.mutex.Lock()
	defer mgr.mutex.Unlock()
}

func (mgr *SettingsManager) initialize(ctx context.Context) error {
	tweakConfigMap := func(options *metav1.ListOptions) {
		cmLabelSelector := fields.ParseSelectorOrDie(partOfArgoCDSelector)
		options.LabelSelector = cmLabelSelector.String()
	}

	eventHandler := cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj interface{}) {
			mgr.invalidateCache()
		},
	}
	indexers := cache.Indexers{
		cache.NamespaceIndex:    cache.MetaNamespaceIndexFunc,
		ByClusterURLIndexer:     byClusterURLIndexerFunc,
	  ByClusterNameIndexer:    byClusterNameIndexerFunc,
	}
	cmInformer := v1.NewFilteredConfigMapInformer(mgr.clientset, mgr.namespace, 3*time.Minute, indexers, tweakConfigMap)
	secretsInformer := v1.NewSecretInformer(mgr.clientset, mgr.namespace, 3*time.Minute, indexers)
	_, err := cmInformer.AddEventHandler(eventHandler)
	if err != nil {
		log.Error(err)
	}

	_, err = secretsInformer.AddEventHandler(eventHandler)
	if err != nil {
		log.Error(err)
	}

	log.Info("Starting configmap/secret informers")
	go func() {
		cmInformer.Run(ctx.Done())
		log.Info("configmap informer cancelled")
	}()
	go func() {
		secretsInformer.Run(ctx.Done())
		log.Info("secrets informer cancelled")
	}()

	if !cache.WaitForCacheSync(ctx.Done(), cmInformer.HasSynced, secretsInformer.HasSynced) {
		return fmt.Errorf("timed out waiting for settings cache to sync")
	}
	log.Info("Configmap/secret informer synced")

	tryNotify := func() {
		newSettings, err := mgr.GetSettings()
		if err != nil {
			log.Warnf("Unable to parse updated settings: %v", err)
		} else {
			mgr.notifySubscribers(newSettings)
		}
	}
	now := time.Now()
	handler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			if metaObj, ok := obj.(metav1.Object); ok {
				if metaObj.GetCreationTimestamp().After(now) {
					tryNotify()
				}
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			oldMeta, oldOk := oldObj.(metav1.Common)
			newMeta, newOk := newObj.(metav1.Common)
			if oldOk && newOk && oldMeta.GetResourceVersion() != newMeta.GetResourceVersion() {
				tryNotify()
			}
		},
	}
	_, err = secretsInformer.AddEventHandler(handler)
	if err != nil {
		log.Error(err)
	}
	_, err = cmInformer.AddEventHandler(handler)
	if err != nil {
		log.Error(err)
	}
	mgr.secrets = v1listers.NewSecretLister(secretsInformer.GetIndexer())
	mgr.secretsInformer = secretsInformer
	mgr.configmaps = v1listers.NewConfigMapLister(cmInformer.GetIndexer())
	return nil
}

func (mgr *SettingsManager) ensureSynced(forceResync bool) error {
	mgr.mutex.Lock()
	defer mgr.mutex.Unlock()
	if !forceResync && mgr.secrets != nil && mgr.configmaps != nil {
		return nil
	}

	if mgr.initContextCancel != nil {
		mgr.initContextCancel()
	}
	ctx, cancel := context.WithCancel(mgr.ctx)
	mgr.initContextCancel = cancel
	return mgr.initialize(ctx)
}

// updateSettingsFromConfigMap transfers settings from a Kubernetes configmap into an ArgoRolloutsSettings struct.
func updateSettingsFromConfigMap(settings *ArgoRolloutsSettings, argoRolloutsCM *apiv1.ConfigMap) {
	settings.DexConfig = argoRolloutsCM.Data[settingDexConfigKey]
	settings.OIDCConfigRAW = argoRolloutsCM.Data[settingsOIDCConfigKey]
	if err := validateExternalURL(argoRolloutsCM.Data[settingURLKey]); err != nil {
		log.Warnf("Failed to validate URL in configmap: %v", err)
	}
	settings.URL = argoRolloutsCM.Data[settingURLKey]
	settings.UserSessionDuration = time.Hour * 24
	if userSessionDurationStr, ok := argoRolloutsCM.Data[userSessionDurationKey]; ok {
		if val, err := timeutil.ParseDuration(userSessionDurationStr); err != nil {
			log.Warnf("Failed to parse '%s' key: %v", userSessionDurationKey, err)
		} else {
			settings.UserSessionDuration = *val
		}
	}
	settings.InClusterEnabled = argoRolloutsCM.Data[inClusterEnabledKey] != "false"
	settings.OIDCTLSInsecureSkipVerify = argoRolloutsCM.Data[oidcTLSInsecureSkipVerifyKey] == "true"
}

// validateExternalURL ensures the external URL that is set on the configmap is valid
func validateExternalURL(u string) error {
	if u == "" {
		return nil
	}
	URL, err := url.Parse(u)
	if err != nil {
		return fmt.Errorf("failed to parse URL: %w", err)
	}
	if URL.Scheme != "http" && URL.Scheme != "https" {
		return fmt.Errorf("URL must include http or https protocol")
	}
	return nil
}

// updateSettingsFromSecret transfers settings from a Kubernetes secret into an ArgoRolloutsSettings struct.
func (mgr *SettingsManager) updateSettingsFromSecret(settings *ArgoRolloutsSettings, argoRolloutsSecret *apiv1.Secret, secrets []*apiv1.Secret) error {
	var errs []error
	//secretKey, ok := argoRolloutsSecret.Data[settingServerSignatureKey]
	//if ok {
	//	settings.ServerSignature = secretKey
	//} else {
	//	errs = append(errs, &incompleteSettingsError{message: "server.secretkey is missing"})
	//}

	// The TLS certificate may be externally managed. We try to load it from an
	// external secret first. If the external secret doesn't exist, we either
	// load it from argo-rollouts-secret or generate (and persist) a self-signed one.
	cert, err := mgr.externalServerTLSCertificate()
	if err != nil {
		errs = append(errs, &incompleteSettingsError{message: fmt.Sprintf("could not read from secret %s/%s: %v", mgr.namespace, externalServerTLSSecretName, err)})
	} else {
		if cert != nil {
			settings.Certificate = cert
			settings.CertificateIsExternal = true
			log.Infof("Loading TLS configuration from secret %s/%s", mgr.namespace, externalServerTLSSecretName)
		} else {
			serverCert, certOk := argoRolloutsSecret.Data[settingServerCertificate]
			serverKey, keyOk := argoRolloutsSecret.Data[settingServerPrivateKey]
			if certOk && keyOk {
				cert, err := tls.X509KeyPair(serverCert, serverKey)
				if err != nil {
					errs = append(errs, &incompleteSettingsError{message: fmt.Sprintf("invalid x509 key pair %s/%s in secret: %s", settingServerCertificate, settingServerPrivateKey, err)})
				} else {
					settings.Certificate = &cert
					settings.CertificateIsExternal = false
				}
			}
		}
	}
	secretValues := make(map[string]string, len(argoRolloutsSecret.Data))
	for _, s := range secrets {
		for k, v := range s.Data {
			secretValues[fmt.Sprintf("%s:%s", s.Name, k)] = string(v)
		}
	}
	for k, v := range argoRolloutsSecret.Data {
		secretValues[k] = string(v)
	}
	settings.Secrets = secretValues
	if len(errs) > 0 {
		return errs[0]
	}

	return nil
}

// externalServerTLSCertificate will try and load a TLS certificate from an
// external secret, instead of tls.crt and tls.key in argo-rollouts-secret. If both
// return values are nil, no external secret has been configured.
func (mgr *SettingsManager) externalServerTLSCertificate() (*tls.Certificate, error) {
	var cert tls.Certificate
	secret, err := mgr.secrets.Secrets(mgr.namespace).Get(externalServerTLSSecretName)
	if err != nil {
		if apierr.IsNotFound(err) {
			return nil, nil
		}
	}
	tlsCert, certOK := secret.Data[settingServerCertificate]
	tlsKey, keyOK := secret.Data[settingServerPrivateKey]
	if certOK && keyOK {
		cert, err = tls.X509KeyPair(tlsCert, tlsKey)
		if err != nil {
			return nil, err
		}
	}
	return &cert, nil
}

// SaveSettings serializes ArgoRolloutsSettings and upserts it into K8s secret/configmap
func (mgr *SettingsManager) SaveSettings(settings *ArgoRolloutsSettings) error {
	err := mgr.updateConfigMap(func(argoRolloutsCM *apiv1.ConfigMap) error {
		if settings.URL != "" {
			argoRolloutsCM.Data[settingURLKey] = settings.URL
		} else {
			delete(argoRolloutsCM.Data, settingURLKey)
		}
		if settings.DexConfig != "" {
			argoRolloutsCM.Data[settingDexConfigKey] = settings.DexConfig
		} else {
			delete(argoRolloutsCM.Data, settings.DexConfig)
		}
		if settings.OIDCConfigRAW != "" {
			argoRolloutsCM.Data[settingsOIDCConfigKey] = settings.OIDCConfigRAW
		} else {
			delete(argoRolloutsCM.Data, settingsOIDCConfigKey)
		}
		return nil
	})
	if err != nil {
		return err
	}

	return mgr.updateSecret(func(argoRolloutsSecret *apiv1.Secret) error {
		argoRolloutsSecret.Data[settingServerSignatureKey] = settings.ServerSignature
		// we only write the certificate to the secret if it's not externally
		// managed.
		if settings.Certificate != nil && !settings.CertificateIsExternal {
			cert, key := tlsutil.EncodeX509KeyPair(*settings.Certificate)
			argoRolloutsSecret.Data[settingServerCertificate] = cert
			argoRolloutsSecret.Data[settingServerPrivateKey] = key
		} else {
			delete(argoRolloutsSecret.Data, settingServerCertificate)
			delete(argoRolloutsSecret.Data, settingServerPrivateKey)
		}
		return nil
	})
}

func (mgr *SettingsManager) SaveTLSCertificateData(ctx context.Context, tlsCertificates map[string]string) error {
	err := mgr.ensureSynced(false)
	if err != nil {
		return err
	}

	certCM, err := mgr.GetConfigMapByName(common.ArgoRolloutsTLSCertsConfigMapName)
	if err != nil {
		return err
	}

	certCM.Data = tlsCertificates
	_, err = mgr.clientset.CoreV1().ConfigMaps(mgr.namespace).Update(ctx, certCM, metav1.UpdateOptions{})
	if err != nil {
		return err
	}

	return mgr.ResyncInformers()
}

type SettingsManagerOpts func(mgs *SettingsManager)

// NewSettingsManager generates a new SettingsManager pointer and returns it
func NewSettingsManager(ctx context.Context, clientset kubernetes.Interface, namespace string, opts ...SettingsManagerOpts) *SettingsManager {
	mgr := &SettingsManager{
		ctx:       ctx,
		clientset: clientset,
		namespace: namespace,
		mutex:     &sync.Mutex{},
	}
	for i := range opts {
		opts[i](mgr)
	}

	return mgr
}

func (mgr *SettingsManager) ResyncInformers() error {
	return mgr.ensureSynced(true)
}

// IsSSOConfigured returns whether or not single-sign-on is configured
func (a *ArgoRolloutsSettings) IsSSOConfigured() bool {
	if a.IsDexConfigured() {
		return true
	}
	if a.OIDCConfig() != nil {
		return true
	}
	return false
}

func (a *ArgoRolloutsSettings) IsDexConfigured() bool {
	if a.URL == "" {
		return false
	}
	dexCfg, err := UnmarshalDexConfig(a.DexConfig)
	if err != nil {
		log.Warnf("invalid dex yaml config: %s", err.Error())
		return false
	}
	return len(dexCfg) > 0
}

// GetServerEncryptionKey generates a new server encryption key using the server signature as a passphrase
func (a *ArgoRolloutsSettings) GetServerEncryptionKey() ([]byte, error) {
	return crypto.KeyFromPassphrase(string(a.ServerSignature))
}

func UnmarshalDexConfig(config string) (map[string]interface{}, error) {
	var dexCfg map[string]interface{}
	err := yaml.Unmarshal([]byte(config), &dexCfg)
	return dexCfg, err
}

func (a *ArgoRolloutsSettings) oidcConfig() *oidcConfig {
	if a.OIDCConfigRAW == "" {
		return nil
	}
	configMap := map[string]interface{}{}
	err := yaml.Unmarshal([]byte(a.OIDCConfigRAW), &configMap)
	if err != nil {
		log.Warnf("invalid oidc config: %v", err)
		return nil
	}

	configMap = ReplaceMapSecrets(configMap, a.Secrets)
	data, err := yaml.Marshal(configMap)
	if err != nil {
		log.Warnf("invalid oidc config: %v", err)
		return nil
	}

	config, err := unmarshalOIDCConfig(string(data))
	if err != nil {
		log.Warnf("invalid oidc config: %v", err)
		return nil
	}

	return &config
}

func (a *ArgoRolloutsSettings) OIDCConfig() *OIDCConfig {
	config := a.oidcConfig()
	if config == nil {
		return nil
	}
	return config.toExported()
}

func unmarshalOIDCConfig(configStr string) (oidcConfig, error) {
	var config oidcConfig
	err := yaml.Unmarshal([]byte(configStr), &config)
	return config, err
}

func ValidateOIDCConfig(configStr string) error {
	_, err := unmarshalOIDCConfig(configStr)
	return err
}

// TLSConfig returns a tls.Config with the configured certificates
func (a *ArgoRolloutsSettings) TLSConfig() *tls.Config {
	if a.Certificate == nil {
		return nil
	}
	certPool := x509.NewCertPool()
	pemCertBytes, _ := tlsutil.EncodeX509KeyPair(*a.Certificate)
	ok := certPool.AppendCertsFromPEM(pemCertBytes)
	if !ok {
		panic("bad certs")
	}
	return &tls.Config{
		RootCAs: certPool,
	}
}

func (a *ArgoRolloutsSettings) IssuerURL() string {
	if oidcConfig := a.OIDCConfig(); oidcConfig != nil {
		return oidcConfig.Issuer
	}
	if a.DexConfig != "" {
		return a.URL + common.DexAPIEndpoint
	}
	return ""
}

// UserInfoGroupsEnabled returns whether group claims should be fetch from UserInfo endpoint
func (a *ArgoRolloutsSettings) UserInfoGroupsEnabled() bool {
	if oidcConfig := a.OIDCConfig(); oidcConfig != nil {
		return oidcConfig.EnableUserInfoGroups
	}
	return false
}

// UserInfoPath returns the sub-path on which the IDP exposes the UserInfo endpoint
func (a *ArgoRolloutsSettings) UserInfoPath() string {
	if oidcConfig := a.OIDCConfig(); oidcConfig != nil {
		return oidcConfig.UserInfoPath
	}
	return ""
}

// UserInfoCacheExpiration returns the expiry time of the UserInfo cache
func (a *ArgoRolloutsSettings) UserInfoCacheExpiration() time.Duration {
	if oidcConfig := a.OIDCConfig(); oidcConfig != nil && oidcConfig.UserInfoCacheExpiration != "" {
		userInfoCacheExpiration, err := time.ParseDuration(oidcConfig.UserInfoCacheExpiration)
		if err != nil {
			log.Warnf("Failed to parse 'oidc.config.userInfoCacheExpiration' key: %v", err)
		}
		return userInfoCacheExpiration
	}
	return 0
}

func (a *ArgoRolloutsSettings) OAuth2ClientID() string {
	if oidcConfig := a.OIDCConfig(); oidcConfig != nil {
		return oidcConfig.ClientID
	}
	if a.DexConfig != "" {
		return common.ArgoRolloutsClientAppID
	}
	return ""
}

// OAuth2AllowedAudiences returns a list of audiences that are allowed for the OAuth2 client. If the user has not
// explicitly configured the list of audiences (or has configured an empty list), then the OAuth2 client ID is returned
// as the only allowed audience. When using the bundled Dex, that client ID is always "argo-rollouts".
func (a *ArgoRolloutsSettings) OAuth2AllowedAudiences() []string {
	if config := a.oidcConfig(); config != nil {
		if len(config.AllowedAudiences) == 0 {
			allowedAudiences := []string{config.ClientID}
			if config.CLIClientID != "" {
				allowedAudiences = append(allowedAudiences, config.CLIClientID)
			}
			return allowedAudiences
		}
		return config.AllowedAudiences
	}
	if a.DexConfig != "" {
		return []string{common.ArgoRolloutsClientAppID, common.ArgoRolloutsCLIClientAppID}
	}
	return nil
}

func (a *ArgoRolloutsSettings) SkipAudienceCheckWhenTokenHasNoAudience() bool {
	if config := a.oidcConfig(); config != nil {
		if config.SkipAudienceCheckWhenTokenHasNoAudience != nil {
			return *config.SkipAudienceCheckWhenTokenHasNoAudience
		}
		return false
	}
	// When using the bundled Dex, the audience check is required. Dex will always send JWTs with an audience.
	return false
}

func (a *ArgoRolloutsSettings) OAuth2ClientSecret() string {
	if oidcConfig := a.OIDCConfig(); oidcConfig != nil {
		return oidcConfig.ClientSecret
	}
	if a.DexConfig != "" {
		return a.DexOAuth2ClientSecret()
	}
	return ""
}

// OIDCTLSConfig returns the TLS config for the OIDC provider. If an external provider is configured, returns a TLS
// config using the root CAs (if any) specified in the OIDC config. If an external OIDC provider is not configured,
// returns the API server TLS config, because the API server proxies requests to Dex.
func (a *ArgoRolloutsSettings) OIDCTLSConfig() *tls.Config {
	var tlsConfig *tls.Config

	oidcConfig := a.OIDCConfig()
	if oidcConfig != nil {
		tlsConfig = &tls.Config{}
		if oidcConfig.RootCA != "" {
			certPool := x509.NewCertPool()
			ok := certPool.AppendCertsFromPEM([]byte(oidcConfig.RootCA))
			if !ok {
				log.Warn("failed to append certificates from PEM: proceeding without custom rootCA")
			} else {
				tlsConfig.RootCAs = certPool
			}
		}
	} else {
		tlsConfig = a.TLSConfig()
	}
	if tlsConfig != nil && a.OIDCTLSInsecureSkipVerify {
		tlsConfig.InsecureSkipVerify = true
	}
	return tlsConfig
}

func appendURLPath(inputURL string, inputPath string) (string, error) {
	u, err := url.Parse(inputURL)
	if err != nil {
		return "", err
	}
	u.Path = path.Join(u.Path, inputPath)
	return u.String(), nil
}

func (a *ArgoRolloutsSettings) RedirectURL() (string, error) {
	return appendURLPath(a.URL, common.CallbackEndpoint)
}

func (a *ArgoRolloutsSettings) DexRedirectURL() (string, error) {
	return appendURLPath(a.URL, common.DexCallbackEndpoint)
}

// DexOAuth2ClientSecret calculates an arbitrary, but predictable OAuth2 client secret string derived
// from the server secret. This is called by the dex startup wrapper (argorollouts-dex rundex), as well
// as the API server, such that they both independently come to the same conclusion of what the
// OAuth2 shared client secret should be.
func (a *ArgoRolloutsSettings) DexOAuth2ClientSecret() string {
	h := sha256.New()
	_, err := h.Write(a.ServerSignature)
	if err != nil {
		panic(err)
	}
	sha := h.Sum(nil)
	return base64.URLEncoding.EncodeToString(sha)[:40]
}

// Subscribe registers a channel in which to subscribe to settings updates
func (mgr *SettingsManager) Subscribe(subCh chan<- *ArgoRolloutsSettings) {
	mgr.mutex.Lock()
	defer mgr.mutex.Unlock()
	mgr.subscribers = append(mgr.subscribers, subCh)
	log.Infof("%v subscribed to settings updates", subCh)
}

// Unsubscribe unregisters a channel from receiving of settings updates
func (mgr *SettingsManager) Unsubscribe(subCh chan<- *ArgoRolloutsSettings) {
	mgr.mutex.Lock()
	defer mgr.mutex.Unlock()
	for i, ch := range mgr.subscribers {
		if ch == subCh {
			mgr.subscribers = append(mgr.subscribers[:i], mgr.subscribers[i+1:]...)
			log.Infof("%v unsubscribed from settings updates", subCh)
			return
		}
	}
}

func (mgr *SettingsManager) notifySubscribers(newSettings *ArgoRolloutsSettings) {
	mgr.mutex.Lock()
	defer mgr.mutex.Unlock()
	if len(mgr.subscribers) > 0 {
		subscribers := make([]chan<- *ArgoRolloutsSettings, len(mgr.subscribers))
		copy(subscribers, mgr.subscribers)
		// make sure subscribes are notified in a separate thread to avoid potential deadlock
		go func() {
			log.Infof("Notifying %d settings subscribers: %v", len(subscribers), subscribers)
			for _, sub := range subscribers {
				sub <- newSettings
			}
		}()
	}
}

func isIncompleteSettingsError(err error) bool {
	var incompleteSettingsErr *incompleteSettingsError
	return errors.As(err, &incompleteSettingsErr)
}

// InitializeSettings is used to initialize empty admin password, signature, certificate etc if missing
func (mgr *SettingsManager) InitializeSettings(insecureModeEnabled bool) (*ArgoRolloutsSettings, error) {
	const letters = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz-"

	rolloutSettings, err := mgr.GetSettings()
	if err != nil && !isIncompleteSettingsError(err) {
		return nil, err
	}
	if rolloutSettings == nil {
		rolloutSettings = &ArgoRolloutsSettings{}
	}
	if rolloutSettings.ServerSignature == nil {
		// set JWT signature
		signature, err := util.MakeSignature(32)
		if err != nil {
			return nil, fmt.Errorf("error setting JWT signature: %w", err)
		}
		rolloutSettings.ServerSignature = signature
		log.Info("Initialized server signature")
	}
	if err != nil {
		return nil, err
	}

	if rolloutSettings.Certificate == nil && !insecureModeEnabled {
		// generate TLS cert
		hosts := []string{
			"localhost",
			"argorollouts-server",
			fmt.Sprintf("argorollouts-server.%s", mgr.namespace),
			fmt.Sprintf("argorollouts-server.%s.svc", mgr.namespace),
			fmt.Sprintf("argorollouts-server.%s.svc.cluster.local", mgr.namespace),
		}
		certOpts := tlsutil.CertOptions{
			Hosts:        hosts,
			Organization: "Argo Rollouts",
			IsCA:         false,
		}
		cert, err := tlsutil.GenerateX509KeyPair(certOpts)
		if err != nil {
			return nil, err
		}
		rolloutSettings.Certificate = cert
		log.Info("Initialized TLS certificate")
	}

	err = mgr.SaveSettings(rolloutSettings)
	if apierrors.IsConflict(err) {
		// assume settings are initialized by another instance of api server
		log.Warnf("conflict when initializing settings. assuming updated by another replica")
		return mgr.GetSettings()
	}
	return rolloutSettings, nil
}

// ReplaceMapSecrets takes a json object and recursively looks for any secret key references in the
// object and replaces the value with the secret value
func ReplaceMapSecrets(obj map[string]interface{}, secretValues map[string]string) map[string]interface{} {
	newObj := make(map[string]interface{})
	for k, v := range obj {
		switch val := v.(type) {
		case map[string]interface{}:
			newObj[k] = ReplaceMapSecrets(val, secretValues)
		case []interface{}:
			newObj[k] = replaceListSecrets(val, secretValues)
		case string:
			newObj[k] = ReplaceStringSecret(val, secretValues)
		default:
			newObj[k] = val
		}
	}
	return newObj
}

func replaceListSecrets(obj []interface{}, secretValues map[string]string) []interface{} {
	newObj := make([]interface{}, len(obj))
	for i, v := range obj {
		switch val := v.(type) {
		case map[string]interface{}:
			newObj[i] = ReplaceMapSecrets(val, secretValues)
		case []interface{}:
			newObj[i] = replaceListSecrets(val, secretValues)
		case string:
			newObj[i] = ReplaceStringSecret(val, secretValues)
		default:
			newObj[i] = val
		}
	}
	return newObj
}

// ReplaceStringSecret checks if given string is a secret key reference ( starts with $ ) and returns corresponding value from provided map
func ReplaceStringSecret(val string, secretValues map[string]string) string {
	if val == "" || !strings.HasPrefix(val, "$") {
		return val
	}
	secretKey := val[1:]
	secretVal, ok := secretValues[secretKey]
	if !ok {
		log.Warnf("config referenced '%s', but key does not exist in secret", val)
		return val
	}
	return strings.TrimSpace(secretVal)
}

func (mgr *SettingsManager) GetNamespace() string {
	return mgr.namespace
}

// Convert group-kind format to <group/kind>, allowed key format examples
// resource.customizations.health.cert-manager.io_Certificate
// resource.customizations.health.Certificate
func convertToOverrideKey(groupKind string) (string, error) {
	parts := strings.Split(groupKind, "_")
	if len(parts) == 2 {
		return fmt.Sprintf("%s/%s", parts[0], parts[1]), nil
	} else if len(parts) == 1 && groupKind != "" {
		return groupKind, nil
	}
	return "", fmt.Errorf("group kind should be in format `resource.customizations.<type>.<group_kind>` or resource.customizations.<type>.<kind>`, got group kind: '%s'", groupKind)
}
