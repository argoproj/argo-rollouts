package auth

import (
	"context"
	"net/http"
	"strings"

	"google.golang.org/grpc/metadata"
)

// AuthCookieName is the cookie that carries the dashboard session token.
const AuthCookieName = "argorollouts.token"

// tokenFromContext extracts a session token from incoming gRPC metadata. It
// checks the "authorization" header (stripping a "Bearer " prefix) first, then
// the auth cookie. It returns "" if no token is present.
func tokenFromContext(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	if vals := md.Get("authorization"); len(vals) > 0 && vals[0] != "" {
		return strings.TrimSpace(stripBearer(vals[0]))
	}
	if vals := md.Get("cookie"); len(vals) > 0 && vals[0] != "" {
		if tok := cookieValue(vals[0], AuthCookieName); tok != "" {
			return tok
		}
	}
	return ""
}

// stripBearer removes a leading "Bearer " prefix (case-insensitive) if present.
func stripBearer(v string) string {
	const prefix = "bearer "
	if len(v) >= len(prefix) && strings.EqualFold(v[:len(prefix)], prefix) {
		return v[len(prefix):]
	}
	return v
}

// cookieValue parses a Cookie header value and returns the named cookie's value.
func cookieValue(cookieHeader, name string) string {
	header := http.Header{}
	header.Add("Cookie", cookieHeader)
	req := http.Request{Header: header}
	c, err := req.Cookie(name)
	if err != nil {
		return ""
	}
	return c.Value
}
