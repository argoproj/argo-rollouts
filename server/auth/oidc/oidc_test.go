package oidc

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/argoproj/argo-rollouts/server/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeURLBuilder struct{}

func (fakeURLBuilder) AuthCodeURL(state string) string { return "https://idp/authorize?state=" + state }

type fakeExchanger struct {
	raw string
	err error
}

func (f fakeExchanger) Exchange(_ context.Context, _ string) (string, error) { return f.raw, f.err }

type fakeVerifier struct {
	claims Claims
	err    error
}

func (f fakeVerifier) Verify(_ context.Context, _ string) (Claims, error) { return f.claims, f.err }

type fakeIssuer struct{ token string }

func (f fakeIssuer) CreateWithGroups(_ string, _ []string, _ time.Duration, _ string) (string, error) {
	return f.token, nil
}

func newHandler() *Handler {
	return &Handler{
		URLBuilder:  fakeURLBuilder{},
		Exchanger:   fakeExchanger{raw: "raw-id-token"},
		Verifier:    fakeVerifier{claims: Claims{Subject: "alice", Groups: []string{"dev"}}},
		Issuer:      fakeIssuer{token: "session.jwt"},
		TokenExpiry: time.Hour,
	}
}

func cookie(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestLoginRedirectsAndSetsState(t *testing.T) {
	h := newHandler()
	req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	rec := httptest.NewRecorder()
	h.Login(rec, req)

	require.Equal(t, http.StatusFound, rec.Code)
	state := cookie(rec, stateCookieName)
	require.NotNil(t, state)
	assert.NotEmpty(t, state.Value)
	assert.True(t, state.HttpOnly)
	assert.Contains(t, rec.Header().Get("Location"), "state="+state.Value)
}

func doCallback(t *testing.T, h *Handler, stateCookieVal, queryState string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=abc&state="+queryState, nil)
	if stateCookieVal != "" {
		req.AddCookie(&http.Cookie{Name: stateCookieName, Value: stateCookieVal})
	}
	rec := httptest.NewRecorder()
	h.Callback(rec, req)
	return rec
}

func TestCallbackSuccessSetsSessionCookie(t *testing.T) {
	h := newHandler()
	rec := doCallback(t, h, "xyz", "xyz")

	assert.Equal(t, http.StatusFound, rec.Code)
	sess := cookie(rec, auth.AuthCookieName)
	require.NotNil(t, sess)
	assert.Equal(t, "session.jwt", sess.Value)
	assert.True(t, sess.HttpOnly)
	assert.Equal(t, http.SameSiteLaxMode, sess.SameSite)
}

func TestCallbackStateMismatchRejected(t *testing.T) {
	h := newHandler()
	rec := doCallback(t, h, "xyz", "DIFFERENT")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Nil(t, cookie(rec, auth.AuthCookieName), "no session on state mismatch")
}

func TestCallbackMissingStateCookieRejected(t *testing.T) {
	h := newHandler()
	rec := doCallback(t, h, "", "xyz")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Nil(t, cookie(rec, auth.AuthCookieName))
}

func TestCallbackExchangeErrorRejected(t *testing.T) {
	h := newHandler()
	h.Exchanger = fakeExchanger{err: errors.New("exchange failed")}
	rec := doCallback(t, h, "xyz", "xyz")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Nil(t, cookie(rec, auth.AuthCookieName))
}

func TestCallbackVerifyErrorRejected(t *testing.T) {
	h := newHandler()
	h.Verifier = fakeVerifier{err: errors.New("bad id token")}
	rec := doCallback(t, h, "xyz", "xyz")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Nil(t, cookie(rec, auth.AuthCookieName))
}
