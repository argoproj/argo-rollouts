package prometheus

import (
	"context"
	"net/http"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
	"golang.org/x/sync/semaphore"
)

// oauth2RoundTripper caches tokens without retaining a previous request's context.
type oauth2RoundTripper struct {
	config      *clientcredentials.Config
	tokenClient *http.Client
	tokenLock   *semaphore.Weighted
	token       *oauth2.Token
}

func newOAuth2RoundTripper(config *clientcredentials.Config, tokenClient *http.Client) http.RoundTripper {
	return &oauth2RoundTripper{
		config:      config,
		tokenClient: tokenClient,
		tokenLock:   semaphore.NewWeighted(1),
	}
}

func (t *oauth2RoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	transport := &oauth2.Transport{
		Source: &oauth2RequestTokenSource{ctx: req.Context(), transport: t},
		Base:   t.tokenClient.Transport,
	}
	return transport.RoundTrip(req)
}

type oauth2RequestTokenSource struct {
	ctx       context.Context
	transport *oauth2RoundTripper
}

func (s *oauth2RequestTokenSource) Token() (*oauth2.Token, error) {
	t := s.transport
	// Waiting for another request's refresh must respect this request's deadline too.
	if err := t.tokenLock.Acquire(s.ctx, 1); err != nil {
		return nil, err
	}
	defer t.tokenLock.Release(1)
	if t.token.Valid() {
		return t.token, nil
	}

	ctx := context.WithValue(s.ctx, oauth2.HTTPClient, t.tokenClient)
	token, err := t.config.Token(ctx)
	if err != nil {
		return nil, err
	}
	t.token = token
	return token, nil
}
