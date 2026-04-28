// Package oauth implements a minimal OAuth 2.1 Authorization Server with PKCE,
// scoped to a single pre-registered client. Tokens are stateless JWTs (HS256)
// so the server stays horizontally scalable; only short-lived authorization
// codes live in memory.
//
// Wire format follows RFC 6749 (OAuth 2.0), RFC 7636 (PKCE), RFC 8414
// (Authorization Server Metadata) and RFC 9728 (Protected Resource Metadata).
package oauth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Config holds the OAuth server configuration. All four required fields
// should be generated with `openssl rand -hex 32` and persisted in .env.
type Config struct {
	Issuer       string // public base URL the resource is served at, e.g. https://mcp.example.com
	ClientID     string // pre-shared client identifier
	ClientSecret string // pre-shared client secret
	SigningKey   string // HMAC key used to sign access/refresh tokens
	AccessTTL    time.Duration
	RefreshTTL   time.Duration
	CodeTTL      time.Duration

	// AllowInsecure permits an `http://` issuer for non-loopback hosts. Off by
	// default — exposing OAuth over plain HTTP leaks every credential and JWT.
	AllowInsecure bool
}

// Service implements the OAuth endpoints and bearer-token middleware.
type Service struct {
	cfg              Config
	codes            sync.Map // string code → *authCode
	tokenLimiter     *rateLimiter
	authorizeLimiter *rateLimiter
}

type authCode struct {
	ClientID    string
	RedirectURI string
	Challenge   string
	Method      string
	Scope       string
	ExpiresAt   time.Time
}

// Claims is the JWT payload for both access and refresh tokens.
type Claims struct {
	Iss      string `json:"iss"`
	Sub      string `json:"sub"`
	Aud      string `json:"aud"`
	Exp      int64  `json:"exp"`
	Iat      int64  `json:"iat"`
	Scope    string `json:"scope,omitempty"`
	ClientID string `json:"client_id,omitempty"`
	TokenUse string `json:"token_use"` // "access" | "refresh"
}

// New validates the config and returns a ready-to-use Service.
// Starts a background goroutine that evicts expired authorization codes.
func New(cfg Config) (*Service, error) {
	cfg.Issuer = strings.TrimRight(cfg.Issuer, "/")
	if cfg.Issuer == "" {
		return nil, errors.New("oauth: OAUTH_ISSUER is required (public URL of this server)")
	}
	issuerURL, err := url.Parse(cfg.Issuer)
	if err != nil || issuerURL.Host == "" {
		return nil, fmt.Errorf("oauth: invalid OAUTH_ISSUER %q", cfg.Issuer)
	}
	if issuerURL.Scheme == "http" && !isLoopback(issuerURL.Hostname()) && !cfg.AllowInsecure {
		return nil, errors.New("oauth: refusing to start with an http:// issuer on a non-loopback host. " +
			"OAuth over plain HTTP leaks every credential and JWT in transit. " +
			"Use https:// (terminate TLS in mcp-observability or with a reverse proxy like Caddy), " +
			"or set OAUTH_ALLOW_INSECURE=true if you really know what you are doing.")
	}
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, errors.New("oauth: OAUTH_CLIENT_ID and OAUTH_CLIENT_SECRET are required")
	}
	if cfg.SigningKey == "" {
		return nil, errors.New("oauth: OAUTH_SIGNING_KEY is required (HMAC secret for JWTs)")
	}
	if cfg.AccessTTL == 0 {
		cfg.AccessTTL = time.Hour
	}
	if cfg.RefreshTTL == 0 {
		cfg.RefreshTTL = 30 * 24 * time.Hour
	}
	if cfg.CodeTTL == 0 {
		cfg.CodeTTL = 60 * time.Second
	}
	s := &Service{
		cfg:              cfg,
		tokenLimiter:     newRateLimiter(5, 10, 10*time.Minute),  // 5 req/min, block 10min after 10 fails
		authorizeLimiter: newRateLimiter(30, 30, 10*time.Minute), // 30 req/min, more permissive (PKCE protects)
	}
	go s.gcLoop()
	return s, nil
}

func isLoopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "[::1]"
}

// Issuer returns the configured public base URL (no trailing slash).
func (s *Service) Issuer() string { return s.cfg.Issuer }

func (s *Service) gcLoop() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for range t.C {
		now := time.Now()
		s.codes.Range(func(k, v any) bool {
			if v.(*authCode).ExpiresAt.Before(now) {
				s.codes.Delete(k)
			}
			return true
		})
	}
}

// ─── JWT (HS256, stdlib only) ────────────────────────────────────────────────

type jwtHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

func (s *Service) signJWT(c Claims) (string, error) {
	h, err := json.Marshal(jwtHeader{Alg: "HS256", Typ: "JWT"})
	if err != nil {
		return "", err
	}
	p, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	head := b64(h) + "." + b64(p)
	return head + "." + hmacSign(head, s.cfg.SigningKey), nil
}

// ValidateBearer parses an Authorization header value ("Bearer <jwt>") and
// returns the Claims if the token is well-formed, signed and unexpired.
func (s *Service) ValidateBearer(authHeader string) (*Claims, error) {
	bearer, ok := strings.CutPrefix(authHeader, "Bearer ")
	if !ok {
		return nil, errors.New("missing bearer token")
	}
	return s.parseJWT(bearer)
}

func (s *Service) parseJWT(token string) (*Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("malformed JWT")
	}
	head := parts[0] + "." + parts[1]
	expected := hmacSign(head, s.cfg.SigningKey)
	if subtle.ConstantTimeCompare([]byte(expected), []byte(parts[2])) != 1 {
		return nil, errors.New("invalid signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid payload: %w", err)
	}
	var c Claims
	if err := json.Unmarshal(payload, &c); err != nil {
		return nil, fmt.Errorf("parse claims: %w", err)
	}
	if time.Now().Unix() >= c.Exp {
		return nil, errors.New("token expired")
	}
	return &c, nil
}

func hmacSign(data, key string) string {
	h := hmac.New(sha256.New, []byte(key))
	h.Write([]byte(data))
	return b64(h.Sum(nil))
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// ─── PKCE (RFC 7636) ─────────────────────────────────────────────────────────

func verifyPKCE(verifier, challenge, method string) bool {
	if method != "S256" {
		return false
	}
	h := sha256.Sum256([]byte(verifier))
	return subtle.ConstantTimeCompare([]byte(b64(h[:])), []byte(challenge)) == 1
}

func randID(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return b64(b), nil
}

// ─── Middleware ──────────────────────────────────────────────────────────────

// Middleware enforces a valid access token on protected routes. Failures
// return 401 with a WWW-Authenticate header pointing to the resource metadata
// document so MCP clients can discover the authorization server (RFC 9728).
func (s *Service) Middleware(next http.Handler) http.Handler {
	challenge := fmt.Sprintf(`Bearer realm="mcp", resource_metadata="%s/.well-known/oauth-protected-resource"`, s.cfg.Issuer)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, err := s.ValidateBearer(r.Header.Get("Authorization"))
		if err != nil {
			w.Header().Set("WWW-Authenticate", challenge+`, error="invalid_token"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if claims.TokenUse != "access" {
			w.Header().Set("WWW-Authenticate", challenge+`, error="invalid_token"`)
			http.Error(w, "wrong token type", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
