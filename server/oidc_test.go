package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

const (
	testOIDCAuthURL     = "https://issuer.example.com/authorize"
	testOIDCTokenURL    = "https://issuer.example.com/token"
	testOIDCClientID    = "test-client"
	testOIDCCallbackURL = "/rollouts/auth/callback?state="
)

func newTestOIDCHandler() *oidcHandler {
	return &oidcHandler{
		oauth2Cfg: &oauth2.Config{
			ClientID: testOIDCClientID,
			Endpoint: oauth2.Endpoint{
				AuthURL:  testOIDCAuthURL,
				TokenURL: testOIDCTokenURL,
			},
			RedirectURL: "http://localhost:3100/rollouts/auth/callback",
			Scopes:      []string{"openid"},
		},
		rootPath: "rollouts",
	}
}

func TestOIDCConfigIsConfigured(t *testing.T) {
	tests := []struct {
		name     string
		config   OIDCConfig
		expected bool
	}{
		{"fully configured", OIDCConfig{IssuerURL: "https://issuer.example.com", ClientID: testOIDCClientID}, true},
		{"missing client ID", OIDCConfig{IssuerURL: "https://issuer.example.com"}, false},
		{"missing issuer URL", OIDCConfig{ClientID: testOIDCClientID}, false},
		{"empty config", OIDCConfig{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.config.IsConfigured())
		})
	}
}

func TestDiscoverOIDC(t *testing.T) {
	t.Run("successful discovery", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/.well-known/openid-configuration" {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]string{
					"authorization_endpoint": testOIDCAuthURL,
					"token_endpoint":         testOIDCTokenURL,
				})
			} else {
				http.NotFound(w, r)
			}
		}))
		defer srv.Close()

		doc, err := discoverOIDC(srv.URL)
		require.NoError(t, err)
		assert.Equal(t, testOIDCAuthURL, doc.AuthorizationEndpoint)
		assert.Equal(t, testOIDCTokenURL, doc.TokenEndpoint)
	})

	t.Run("missing endpoints", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{})
		}))
		defer srv.Close()

		_, err := discoverOIDC(srv.URL)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "missing required endpoints")
	})

	t.Run("server error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		_, err := discoverOIDC(srv.URL)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "status 500")
	})
}

func TestGenerateState(t *testing.T) {
	state1, err := generateState()
	require.NoError(t, err)
	assert.NotEmpty(t, state1)

	state2, err := generateState()
	require.NoError(t, err)
	assert.NotEqual(t, state1, state2, "states should be unique")
}

func TestHandleLogin(t *testing.T) {
	handler := newTestOIDCHandler()

	req := httptest.NewRequest(http.MethodGet, "/rollouts/auth/login", nil)
	w := httptest.NewRecorder()

	handler.handleLogin(w, req)

	assert.Equal(t, http.StatusFound, w.Code)
	location := w.Header().Get("Location")
	assert.Contains(t, location, testOIDCAuthURL)
	assert.Contains(t, location, "client_id="+testOIDCClientID)
	assert.Contains(t, location, "scope=openid")
	assert.Contains(t, location, "state=")
}

func TestHandleCallback(t *testing.T) {
	handler := newTestOIDCHandler()

	t.Run("missing state parameter", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/rollouts/auth/callback?code=abc", nil)
		w := httptest.NewRecorder()

		handler.handleCallback(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Missing state parameter")
	})

	t.Run("invalid state parameter", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, testOIDCCallbackURL+"invalid&code=abc", nil)
		w := httptest.NewRecorder()

		handler.handleCallback(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Invalid state parameter")
	})

	t.Run("missing authorization code", func(t *testing.T) {
		state, _ := generateState()
		handler.states.Store(state, time.Now().Add(5*time.Minute))

		req := httptest.NewRequest(http.MethodGet, testOIDCCallbackURL+state, nil)
		w := httptest.NewRecorder()

		handler.handleCallback(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Missing authorization code")
	})

	t.Run("OIDC provider error", func(t *testing.T) {
		state, _ := generateState()
		handler.states.Store(state, time.Now().Add(5*time.Minute))

		req := httptest.NewRequest(http.MethodGet, testOIDCCallbackURL+state+"&error=access_denied&error_description=User+denied+access", nil)
		w := httptest.NewRecorder()

		handler.handleCallback(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("expired state", func(t *testing.T) {
		state, _ := generateState()
		handler.states.Store(state, time.Now().Add(-1*time.Minute))

		req := httptest.NewRequest(http.MethodGet, testOIDCCallbackURL+state+"&code=abc", nil)
		w := httptest.NewRecorder()

		handler.handleCallback(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "expired")
	})
}

func TestJsonString(t *testing.T) {
	assert.Equal(t, `"hello"`, jsonString("hello"))
	assert.Equal(t, `"with \"quotes\""`, jsonString(`with "quotes"`))
	assert.Equal(t, `"\u003cscript\u003e"`, jsonString("<script>"))
}

func TestNewOIDCHandler(t *testing.T) {
	t.Run("successful creation", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{
				"authorization_endpoint": testOIDCAuthURL,
				"token_endpoint":         testOIDCTokenURL,
			})
		}))
		defer srv.Close()

		handler, err := newOIDCHandler(OIDCConfig{
			IssuerURL:    srv.URL,
			ClientID:     testOIDCClientID,
			ClientSecret: "secret",
			RedirectURL:  "http://localhost:3100/callback",
		}, "rollouts")

		require.NoError(t, err)
		assert.NotNil(t, handler)
		assert.Equal(t, testOIDCClientID, handler.oauth2Cfg.ClientID)
		assert.Equal(t, "secret", handler.oauth2Cfg.ClientSecret)
		assert.Equal(t, "rollouts", handler.rootPath)
		assert.Equal(t, []string{"openid", "profile", "email"}, handler.oauth2Cfg.Scopes)
	})

	t.Run("custom scopes", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{
				"authorization_endpoint": testOIDCAuthURL,
				"token_endpoint":         testOIDCTokenURL,
			})
		}))
		defer srv.Close()

		handler, err := newOIDCHandler(OIDCConfig{
			IssuerURL: srv.URL,
			ClientID:  testOIDCClientID,
			Scopes:    []string{"openid", "groups"},
		}, "")

		require.NoError(t, err)
		assert.Equal(t, []string{"openid", "groups"}, handler.oauth2Cfg.Scopes)
	})

	t.Run("discovery failure", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		_, err := newOIDCHandler(OIDCConfig{
			IssuerURL: srv.URL,
			ClientID:  testOIDCClientID,
		}, "")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "OIDC discovery failed")
	})
}

func TestHandleCallback_SuccessfulExchange(t *testing.T) {
	// Mock token server that returns an id_token
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "access-token-123",
			"token_type":   "Bearer",
			"id_token":     "test-id-token-value",
		})
	}))
	defer tokenServer.Close()

	handler := &oidcHandler{
		oauth2Cfg: &oauth2.Config{
			ClientID: testOIDCClientID,
			Endpoint: oauth2.Endpoint{
				AuthURL:  testOIDCAuthURL,
				TokenURL: tokenServer.URL,
			},
			RedirectURL: "http://localhost:3100/rollouts/auth/callback",
			Scopes:      []string{"openid"},
		},
		rootPath: "rollouts",
	}

	state, _ := generateState()
	handler.states.Store(state, time.Now().Add(5*time.Minute))

	req := httptest.NewRequest(http.MethodGet, testOIDCCallbackURL+state+"&code=valid-auth-code", nil)
	w := httptest.NewRecorder()

	handler.handleCallback(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "localStorage.setItem")
	assert.Contains(t, body, "auth_token")
	assert.Contains(t, body, "test-id-token-value")
}

func TestHandleCallback_ExchangeFailure(t *testing.T) {
	// Mock token server that returns an error
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "invalid_grant"}`))
	}))
	defer tokenServer.Close()

	handler := &oidcHandler{
		oauth2Cfg: &oauth2.Config{
			ClientID: testOIDCClientID,
			Endpoint: oauth2.Endpoint{
				AuthURL:  testOIDCAuthURL,
				TokenURL: tokenServer.URL,
			},
			RedirectURL: "http://localhost:3100/rollouts/auth/callback",
			Scopes:      []string{"openid"},
		},
		rootPath: "rollouts",
	}

	state, _ := generateState()
	handler.states.Store(state, time.Now().Add(5*time.Minute))

	req := httptest.NewRequest(http.MethodGet, testOIDCCallbackURL+state+"&code=invalid-code", nil)
	w := httptest.NewRecorder()

	handler.handleCallback(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "Failed to exchange authorization code")
}

func TestHandleCallback_MissingIDToken(t *testing.T) {
	// Mock token server that returns no id_token
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "access-token-123",
			"token_type":   "Bearer",
		})
	}))
	defer tokenServer.Close()

	handler := &oidcHandler{
		oauth2Cfg: &oauth2.Config{
			ClientID: testOIDCClientID,
			Endpoint: oauth2.Endpoint{
				AuthURL:  testOIDCAuthURL,
				TokenURL: tokenServer.URL,
			},
			RedirectURL: "http://localhost:3100/rollouts/auth/callback",
			Scopes:      []string{"openid"},
		},
		rootPath: "rollouts",
	}

	state, _ := generateState()
	handler.states.Store(state, time.Now().Add(5*time.Minute))

	req := httptest.NewRequest(http.MethodGet, testOIDCCallbackURL+state+"&code=valid-code", nil)
	w := httptest.NewRecorder()

	handler.handleCallback(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "No ID token received")
}

func TestDiscoverOIDC_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not valid json"))
	}))
	defer srv.Close()

	_, err := discoverOIDC(srv.URL)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse")
}

func TestRegisterOIDCRoutes(t *testing.T) {
	handler := newTestOIDCHandler()

	t.Run("registers routes with root path", func(t *testing.T) {
		mux := http.NewServeMux()
		handler.registerOIDCRoutes(mux, "rollouts")

		req := httptest.NewRequest(http.MethodGet, "/rollouts/auth/login", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		assert.Equal(t, http.StatusFound, w.Code)
	})

	t.Run("registers routes without root path", func(t *testing.T) {
		mux := http.NewServeMux()
		handler.registerOIDCRoutes(mux, "")

		req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		assert.Equal(t, http.StatusFound, w.Code)
	})
}
