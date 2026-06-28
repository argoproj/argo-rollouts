package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeCredVerifier struct{ err error }

func (f fakeCredVerifier) VerifyUsernamePassword(_ context.Context, _, _ string) error {
	return f.err
}

type fakeIssuer struct {
	token    string
	err      error
	seenSub  string
	seenExp  time.Duration
}

func (f *fakeIssuer) Create(subject string, expiry time.Duration, _ string) (string, error) {
	f.seenSub = subject
	f.seenExp = expiry
	return f.token, f.err
}

func postLogin(h http.Handler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestLoginSuccessSetsCookieAndToken(t *testing.T) {
	h := &LoginHandler{Verifier: fakeCredVerifier{}, Issuer: &fakeIssuer{token: "tok.123"}, TokenExpiry: time.Hour}
	rec := postLogin(h, `{"username":"alice","password":"s3cret"}`)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "tok.123", resp["token"])

	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, AuthCookieName, cookies[0].Name)
	assert.Equal(t, "tok.123", cookies[0].Value)
	assert.True(t, cookies[0].HttpOnly)
	assert.Equal(t, http.SameSiteStrictMode, cookies[0].SameSite)
}

func TestLoginBadCredentialsGeneric(t *testing.T) {
	// Different underlying errors must produce identical responses (no enumeration).
	underlyings := []error{
		errors.New(`account "ghost" not found`),
		errors.New(`account "bob" is disabled`),
		errors.New("crypto/bcrypt: hashedPassword is not the hash of the given password"),
	}
	var firstCode int
	var firstBody []byte
	for i, underlying := range underlyings {
		h := &LoginHandler{Verifier: fakeCredVerifier{err: underlying}, Issuer: &fakeIssuer{token: "x"}, TokenExpiry: time.Hour}
		rec := postLogin(h, `{"username":"u","password":"p"}`)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
		assert.Empty(t, rec.Result().Cookies(), "no cookie on failure")
		assert.NotContains(t, rec.Body.String(), "not found")
		assert.NotContains(t, rec.Body.String(), "disabled")
		assert.Equal(t, "invalid username or password\n", rec.Body.String())
		if i == 0 {
			firstCode = rec.Code
			firstBody = append([]byte(nil), rec.Body.Bytes()...)
		} else {
			assert.Equal(t, firstCode, rec.Code, "status must be byte-identical across variants (no enumeration)")
			assert.Equal(t, firstBody, rec.Body.Bytes(), "body must be byte-identical across variants (no enumeration)")
		}
	}
}

func TestLoginRejectsNonPost(t *testing.T) {
	h := &LoginHandler{Verifier: fakeCredVerifier{}, Issuer: &fakeIssuer{}, TokenExpiry: time.Hour}
	req := httptest.NewRequest(http.MethodGet, "/api/login", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestLoginRejectsMalformedBody(t *testing.T) {
	h := &LoginHandler{Verifier: fakeCredVerifier{}, Issuer: &fakeIssuer{}, TokenExpiry: time.Hour}
	rec := postLogin(h, `{not json`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestLoginSubjectAndExpiryPassedToIssuer(t *testing.T) {
	iss := &fakeIssuer{token: "t"}
	h := &LoginHandler{Verifier: fakeCredVerifier{}, Issuer: iss, TokenExpiry: 2 * time.Hour}
	postLogin(h, `{"username":"carol","password":"p"}`)
	assert.Equal(t, "carol", iss.seenSub)
	assert.Equal(t, 2*time.Hour, iss.seenExp)
}

func TestLogoutClearsCookie(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/logout", nil)
	rec := httptest.NewRecorder()
	LogoutHandler(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, AuthCookieName, cookies[0].Name)
	assert.Equal(t, "", cookies[0].Value)
	assert.True(t, cookies[0].MaxAge < 0, "logout cookie expires immediately")
	assert.Equal(t, http.SameSiteStrictMode, cookies[0].SameSite, "logout cookie must set SameSite=Strict to prevent forced-logout CSRF")
}

func TestLoginCookieSecureFlag(t *testing.T) {
	// The session cookie is always marked Secure so the token never travels
	// over cleartext HTTP. localhost remains usable (browser secure context).
	h := &LoginHandler{Verifier: fakeCredVerifier{}, Issuer: &fakeIssuer{token: "tok"}, TokenExpiry: time.Hour}
	rec := postLogin(h, `{"username":"alice","password":"s3cret"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.True(t, cookies[0].Secure, "session cookie must always carry the Secure flag")
}
