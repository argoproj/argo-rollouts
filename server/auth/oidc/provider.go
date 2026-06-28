package oidc

import (
	"context"
	"fmt"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// ProviderConfig configures a real OIDC provider Handler.
type ProviderConfig struct {
	Issuer        string
	ClientID      string
	ClientSecret  string
	RedirectURL   string
	Scopes        []string
	SessionIssuer TokenIssuerWithGroups
	TokenExpiry   time.Duration
	Secure        bool
}

// oauth2Adapter implements AuthCodeURLBuilder + CodeExchanger over oauth2.Config.
type oauth2Adapter struct{ cfg *oauth2.Config }

func newOAuth2Adapter(clientID, clientSecret, authURL, tokenURL, redirectURL string, scopes []string) *oauth2Adapter {
	return &oauth2Adapter{cfg: &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     oauth2.Endpoint{AuthURL: authURL, TokenURL: tokenURL},
		RedirectURL:  redirectURL,
		Scopes:       scopes,
	}}
}

func (a *oauth2Adapter) AuthCodeURL(state string) string { return a.cfg.AuthCodeURL(state) }

func (a *oauth2Adapter) Exchange(ctx context.Context, code string) (string, error) {
	tok, err := a.cfg.Exchange(ctx, code)
	if err != nil {
		return "", err
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok || raw == "" {
		return "", fmt.Errorf("no id_token in token response")
	}
	return raw, nil
}

// verifierAdapter implements IDTokenVerifier over a go-oidc verifier.
type verifierAdapter struct{ v *gooidc.IDTokenVerifier }

func (a *verifierAdapter) Verify(ctx context.Context, raw string) (Claims, error) {
	idToken, err := a.v.Verify(ctx, raw)
	if err != nil {
		return Claims{}, err
	}
	var c struct {
		Subject string   `json:"sub"`
		Groups  []string `json:"groups"`
	}
	if err := idToken.Claims(&c); err != nil {
		return Claims{}, err
	}
	if c.Subject == "" {
		c.Subject = idToken.Subject
	}
	return Claims{Subject: c.Subject, Groups: c.Groups}, nil
}

// NewProvider discovers the issuer and returns a ready Handler.
func NewProvider(ctx context.Context, cfg ProviderConfig) (*Handler, error) {
	if cfg.Issuer == "" {
		return nil, fmt.Errorf("oidc: issuer is required")
	}
	provider, err := gooidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc: discover %s: %w", cfg.Issuer, err)
	}
	oa := newOAuth2Adapter(cfg.ClientID, cfg.ClientSecret, provider.Endpoint().AuthURL, provider.Endpoint().TokenURL, cfg.RedirectURL, cfg.Scopes)
	verifier := provider.Verifier(&gooidc.Config{ClientID: cfg.ClientID})
	return &Handler{
		URLBuilder:  oa,
		Exchanger:   oa,
		Verifier:    &verifierAdapter{v: verifier},
		Issuer:      cfg.SessionIssuer,
		TokenExpiry: cfg.TokenExpiry,
		Secure:      cfg.Secure,
	}, nil
}
