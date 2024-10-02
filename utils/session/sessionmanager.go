package session

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/golang-jwt/jwt/v4"
	log "github.com/sirupsen/logrus"
	"github.com/argoproj/argo-rollouts/common"
	"github.com/argoproj/argo-rollouts/utils/dex"
	"github.com/argoproj/argo-rollouts/utils/env"
	httputil "github.com/argoproj/argo-rollouts/utils/http"
	jwtutil "github.com/argoproj/argo-rollouts/utils/jwt"
	oidcutil "github.com/argoproj/argo-rollouts/utils/oidc"
	"github.com/argoproj/argo-rollouts/utils/settings"
)

// SessionManager generates and validates JWT tokens for login sessions.
type SessionManager struct {
	settingsMgr                   *settings.SettingsManager
	client                        *http.Client
	prov                          oidcutil.Provider
	storage                       UserStateStorage
	sleep                         func(d time.Duration)
	verificationDelayNoiseEnabled bool
	failedLock                    sync.RWMutex
}

// LoginAttempts is a timestamped counter for failed login attempts
type LoginAttempts struct {
	// Time of the last failed login
	LastFailed time.Time `json:"lastFailed"`
	// Number of consecutive login failures
	FailCount int `json:"failCount"`
}

const (
	// SessionManagerClaimsIssuer fills the "iss" field of the token.
	SessionManagerClaimsIssuer = "argo-rollouts"
	AuthErrorCtxKey            = "auth-error"

	accountDisabled             = "Account %s is disabled"
	autoRegenerateTokenDuration = time.Minute * 5
)

const (
	// Maximum length of username, too keep the cache's memory signature low
	maxUsernameLength = 32
	// The default maximum session cache size
	defaultMaxCacheSize = 10000
	// The default number of maximum login failures before delay kicks in
	defaultMaxLoginFailures = 5
	// The default time in seconds for the failure window
	defaultFailureWindow = 300
	// The password verification delay max
	verificationDelayNoiseMin = 500 * time.Millisecond
	// The password verification delay max
	verificationDelayNoiseMax = 1000 * time.Millisecond

	// environment variables to control rate limiter behaviour:

	// Max number of login failures before login delay kicks in
	envLoginMaxFailCount = "ARGOROLLOUTS_SESSION_FAILURE_MAX_FAIL_COUNT"

	// Number of maximum seconds the login is allowed to delay for. Default: 300 (5 minutes).
	envLoginFailureWindowSeconds = "ARGOROLLOUTS_SESSION_FAILURE_WINDOW_SECONDS"

	// Max number of stored usernames
	envLoginMaxCacheSize = "ARGOROLLOUTS_SESSION_MAX_CACHE_SIZE"
)

// Returns the maximum cache size as number of entries
func getMaximumCacheSize() int {
	return env.ParseNumFromEnv(envLoginMaxCacheSize, defaultMaxCacheSize, 1, math.MaxInt32)
}

// Returns the maximum number of login failures before login delay kicks in
func getMaxLoginFailures() int {
	return env.ParseNumFromEnv(envLoginMaxFailCount, defaultMaxLoginFailures, 1, math.MaxInt32)
}

// Returns the number of maximum seconds the login is allowed to delay for
func getLoginFailureWindow() time.Duration {
	return time.Duration(env.ParseNumFromEnv(envLoginFailureWindowSeconds, defaultFailureWindow, 0, math.MaxInt32))
}

// NewSessionManager creates a new session manager from Argo CD settings
func NewSessionManager(settingsMgr *settings.SettingsManager, dexServerAddr string, dexTlsConfig *dex.DexTLSConfig, storage UserStateStorage) *SessionManager {
	s := SessionManager{
		settingsMgr:                   settingsMgr,
		storage:                       storage,
		sleep:                         time.Sleep,
		verificationDelayNoiseEnabled: true,
	}
	settings, err := settingsMgr.GetSettings()
	if err != nil {
		panic(err)
	}

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		Dial: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).Dial,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	s.client = &http.Client{
		Transport: transport,
	}

	if settings.DexConfig != "" {
		transport.TLSClientConfig = dex.TLSConfig(dexTlsConfig)
		addrWithProto := dex.DexServerAddressWithProtocol(dexServerAddr, dexTlsConfig)
		s.client.Transport = dex.NewDexRewriteURLRoundTripper(addrWithProto, s.client.Transport)
	} else {
		transport.TLSClientConfig = settings.OIDCTLSConfig()
	}
	if os.Getenv(common.EnvVarSSODebug) == "1" {
		s.client.Transport = httputil.DebugTransport{T: s.client.Transport}
	}

	return &s
}

// Create creates a new token for a given subject (user) and returns it as a string.
// Passing a value of `0` for secondsBeforeExpiry creates a token that never expires.
// The id parameter holds an optional unique JWT token identifier and stored as a standard claim "jti" in the JWT token.
func (mgr *SessionManager) Create(subject string, secondsBeforeExpiry int64, id string) (string, error) {
	// Create a new token object, specifying signing method and the claims
	// you would like it to contain.
	now := time.Now().UTC()
	claims := jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(now),
		Issuer:    SessionManagerClaimsIssuer,
		NotBefore: jwt.NewNumericDate(now),
		Subject:   subject,
		ID:        id,
	}
	if secondsBeforeExpiry > 0 {
		expires := now.Add(time.Duration(secondsBeforeExpiry) * time.Second)
		claims.ExpiresAt = jwt.NewNumericDate(expires)
	}

	return mgr.signClaims(claims)
}

func (mgr *SessionManager) signClaims(claims jwt.Claims) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	settings, err := mgr.settingsMgr.GetSettings()
	if err != nil {
		return "", err
	}
	return token.SignedString(settings.ServerSignature)
}

// GetLoginFailures retrieves the login failure information from the cache. Any modifications to the LoginAttemps map must be done in a thread-safe manner.
func (mgr *SessionManager) GetLoginFailures() map[string]LoginAttempts {
	// Get failures from the cache
	var failures map[string]LoginAttempts
	err := mgr.storage.GetLoginAttempts(&failures)
	if err != nil {
		log.Errorf("Could not retrieve login attempts: %v", err)
		failures = make(map[string]LoginAttempts)
	}

	return failures
}

func expireOldFailedAttempts(maxAge time.Duration, failures map[string]LoginAttempts) int {
	expiredCount := 0
	for key, attempt := range failures {
		if time.Since(attempt.LastFailed) > maxAge*time.Second {
			expiredCount += 1
			delete(failures, key)
		}
	}
	return expiredCount
}

// Get the current login failure attempts for given username
func (mgr *SessionManager) getFailureCount(username string) LoginAttempts {
	mgr.failedLock.RLock()
	defer mgr.failedLock.RUnlock()
	failures := mgr.GetLoginFailures()
	attempt, ok := failures[username]
	if !ok {
		attempt = LoginAttempts{FailCount: 0}
	}
	return attempt
}

// Calculate a login delay for the given login attempt
func (mgr *SessionManager) exceededFailedLoginAttempts(attempt LoginAttempts) bool {
	maxFails := getMaxLoginFailures()
	failureWindow := getLoginFailureWindow()

	// Whether we are in the failure window for given attempt
	inWindow := func() bool {
		if failureWindow == 0 || time.Since(attempt.LastFailed).Seconds() <= float64(failureWindow) {
			return true
		}
		return false
	}

	// If we reached max failed attempts within failure window, we need to calc the delay
	if attempt.FailCount >= maxFails && inWindow() {
		return true
	}

	return false
}

// AuthMiddlewareFunc returns a function that can be used as an
// authentication middleware for HTTP requests.
func (mgr *SessionManager) AuthMiddlewareFunc(disabled bool) func(http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		return WithAuthMiddleware(disabled, mgr, h)
	}
}

// TokenVerifier defines the contract to invoke token
// verification logic
type TokenVerifier interface {
	VerifyToken(token string) (jwt.Claims, string, error)
}

// WithAuthMiddleware is an HTTP middleware used to ensure incoming
// requests are authenticated before invoking the target handler. If
// disabled is true, it will just invoke the next handler in the chain.
func WithAuthMiddleware(disabled bool, authn TokenVerifier, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !disabled {
			cookies := r.Cookies()
			tokenString, err := httputil.JoinCookies(common.AuthCookieName, cookies)
			if err != nil {
				http.Error(w, "Auth cookie not found", http.StatusBadRequest)
				return
			}
			claims, _, err := authn.VerifyToken(tokenString)
			if err != nil {
				http.Error(w, "Invalid token", http.StatusUnauthorized)
				return
			}
			ctx := r.Context()
			// Add claims to the context to inspect for RBAC
			// nolint:staticcheck
			ctx = context.WithValue(ctx, "claims", claims)
			r = r.WithContext(ctx)
		}
		next.ServeHTTP(w, r)
	})
}

// VerifyToken verifies if a token is correct. Tokens can be issued either from us or by an IDP.
// We choose how to verify based on the issuer.
func (mgr *SessionManager) VerifyToken(tokenString string) (jwt.Claims, string, error) {
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	var claims jwt.RegisteredClaims
	_, _, err := parser.ParseUnverified(tokenString, &claims)
	if err != nil {
		return nil, "", err
	}
	switch claims.Issuer {
	default:
		// IDP signed token
		prov, err := mgr.provider()
		if err != nil {
			return nil, "", err
		}

		argoSettings, err := mgr.settingsMgr.GetSettings()
		if err != nil {
			return nil, "", fmt.Errorf("cannot access settings while verifying the token: %w", err)
		}
		if argoSettings == nil {
			return nil, "", fmt.Errorf("settings are not available while verifying the token")
		}

		idToken, err := prov.Verify(tokenString, argoSettings)
		// The token verification has failed. If the token has expired, we will
		// return a dummy claims only containing a value for the issuer, so the
		// UI can handle expired tokens appropriately.
		if err != nil {
			log.Warnf("Failed to verify token: %s", err)
			tokenExpiredError := &oidc.TokenExpiredError{}
			if errors.As(err, &tokenExpiredError) {
				claims = jwt.RegisteredClaims{
					Issuer: "sso",
				}
				return claims, "", common.TokenVerificationErr
			}
			return nil, "", common.TokenVerificationErr
		}

		var claims jwt.MapClaims
		err = idToken.Claims(&claims)
		if err != nil {
			return nil, "", err
		}
		return claims, "", nil
	}
}

func (mgr *SessionManager) provider() (oidcutil.Provider, error) {
	if mgr.prov != nil {
		return mgr.prov, nil
	}
	settings, err := mgr.settingsMgr.GetSettings()
	if err != nil {
		return nil, err
	}
	if !settings.IsSSOConfigured() {
		return nil, fmt.Errorf("SSO is not configured")
	}
	mgr.prov = oidcutil.NewOIDCProvider(settings.IssuerURL(), mgr.client)
	return mgr.prov, nil
}

func (mgr *SessionManager) RevokeToken(ctx context.Context, id string, expiringAt time.Duration) error {
	return mgr.storage.RevokeToken(ctx, id, expiringAt)
}

func LoggedIn(ctx context.Context) bool {
	return Sub(ctx) != "" && ctx.Value(AuthErrorCtxKey) == nil
}

// Username is a helper to extract a human readable username from a context
func Username(ctx context.Context) string {
	mapClaims, ok := mapClaims(ctx)
	if !ok {
		return ""
	}
	switch jwtutil.StringField(mapClaims, "iss") {
	case SessionManagerClaimsIssuer:
		return jwtutil.StringField(mapClaims, "sub")
	default:
		return jwtutil.StringField(mapClaims, "email")
	}
}

func Iss(ctx context.Context) string {
	mapClaims, ok := mapClaims(ctx)
	if !ok {
		return ""
	}
	return jwtutil.StringField(mapClaims, "iss")
}

func Iat(ctx context.Context) (time.Time, error) {
	mapClaims, ok := mapClaims(ctx)
	if !ok {
		return time.Time{}, errors.New("unable to extract token claims")
	}
	return jwtutil.IssuedAtTime(mapClaims)
}

func Sub(ctx context.Context) string {
	mapClaims, ok := mapClaims(ctx)
	if !ok {
		return ""
	}
	return jwtutil.StringField(mapClaims, "sub")
}

func Groups(ctx context.Context, scopes []string) []string {
	mapClaims, ok := mapClaims(ctx)
	if !ok {
		return nil
	}
	return jwtutil.GetGroups(mapClaims, scopes)
}

func mapClaims(ctx context.Context) (jwt.MapClaims, bool) {
	claims, ok := ctx.Value("claims").(jwt.Claims)
	if !ok {
		return nil, false
	}
	mapClaims, err := jwtutil.MapClaims(claims)
	if err != nil {
		return nil, false
	}
	return mapClaims, true
}
