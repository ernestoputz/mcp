package oauth

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Maximum body size accepted on /token. Real OAuth requests are well under 1KB;
// 4KB is generous and bounds memory if someone tries a body bomb.
const maxTokenBodyBytes = 4 * 1024

// HandleAuthorizationServerMetadata serves /.well-known/oauth-authorization-server (RFC 8414).
func (s *Service) HandleAuthorizationServerMetadata(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                s.cfg.Issuer,
		"authorization_endpoint":                s.cfg.Issuer + "/authorize",
		"token_endpoint":                        s.cfg.Issuer + "/token",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_post", "client_secret_basic"},
		"scopes_supported":                      []string{"mcp"},
	})
}

// HandleProtectedResourceMetadata serves /.well-known/oauth-protected-resource (RFC 9728).
func (s *Service) HandleProtectedResourceMetadata(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":                 s.cfg.Issuer,
		"authorization_servers":    []string{s.cfg.Issuer},
		"scopes_supported":         []string{"mcp"},
		"bearer_methods_supported": []string{"header"},
	})
}

// HandleAuthorize implements GET /authorize.
// Validates client_id, PKCE challenge and redirect_uri, mints a one-shot code,
// and 302-redirects back to redirect_uri with ?code=&state=.
func (s *Service) HandleAuthorize(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if ok, retry := s.authorizeLimiter.Allow(ip); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(retry))
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}
	q := r.URL.Query()
	clientID := q.Get("client_id")
	redirectURI := q.Get("redirect_uri")
	responseType := q.Get("response_type")
	state := q.Get("state")
	challenge := q.Get("code_challenge")
	method := q.Get("code_challenge_method")
	scope := q.Get("scope")

	if responseType != "code" {
		http.Error(w, "unsupported_response_type", http.StatusBadRequest)
		return
	}
	if subtle.ConstantTimeCompare([]byte(clientID), []byte(s.cfg.ClientID)) != 1 {
		http.Error(w, "invalid_client", http.StatusBadRequest)
		return
	}
	if redirectURI == "" {
		http.Error(w, "missing redirect_uri", http.StatusBadRequest)
		return
	}
	u, err := url.Parse(redirectURI)
	if err != nil || (u.Scheme != "https" && u.Host != "localhost" && u.Scheme != "http") {
		http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
		return
	}
	if challenge == "" || method != "S256" {
		http.Error(w, "PKCE required: code_challenge with code_challenge_method=S256", http.StatusBadRequest)
		return
	}

	code, err := randID(32)
	if err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	s.codes.Store(code, &authCode{
		ClientID:    clientID,
		RedirectURI: redirectURI,
		Challenge:   challenge,
		Method:      method,
		Scope:       scope,
		ExpiresAt:   time.Now().Add(s.cfg.CodeTTL),
	})

	rq := u.Query()
	rq.Set("code", code)
	if state != "" {
		rq.Set("state", state)
	}
	u.RawQuery = rq.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

// HandleToken implements POST /token for the authorization_code and
// refresh_token grants. Client credentials may come from the form body or
// HTTP Basic auth (RFC 6749 §2.3.1).
func (s *Service) HandleToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOAuthError(w, http.StatusMethodNotAllowed, "invalid_request", "POST required")
		return
	}

	ip := clientIP(r)
	if ok, retry := s.tokenLimiter.Allow(ip); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(retry))
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	// Bound the request body so a large payload cannot exhaust memory in ParseForm.
	r.Body = http.MaxBytesReader(w, r.Body, maxTokenBodyBytes)
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "request body too large or malformed")
		return
	}

	clientID, clientSecret := r.Form.Get("client_id"), r.Form.Get("client_secret")
	if cid, csec, ok := r.BasicAuth(); ok {
		clientID, clientSecret = cid, csec
	}
	if subtle.ConstantTimeCompare([]byte(clientID), []byte(s.cfg.ClientID)) != 1 ||
		subtle.ConstantTimeCompare([]byte(clientSecret), []byte(s.cfg.ClientSecret)) != 1 {
		s.tokenLimiter.Fail(ip)
		w.Header().Set("WWW-Authenticate", `Basic realm="mcp"`)
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "")
		return
	}

	switch r.Form.Get("grant_type") {
	case "authorization_code":
		s.handleAuthCodeGrant(w, r, ip)
	case "refresh_token":
		s.handleRefreshGrant(w, r, ip)
	default:
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", r.Form.Get("grant_type"))
	}
}

func (s *Service) handleAuthCodeGrant(w http.ResponseWriter, r *http.Request, ip string) {
	code := r.Form.Get("code")
	verifier := r.Form.Get("code_verifier")
	redirectURI := r.Form.Get("redirect_uri")

	v, ok := s.codes.LoadAndDelete(code)
	if !ok {
		s.tokenLimiter.Fail(ip)
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "code not found or already used")
		return
	}
	ac := v.(*authCode)
	if time.Now().After(ac.ExpiresAt) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "code expired")
		return
	}
	if redirectURI != "" && redirectURI != ac.RedirectURI {
		s.tokenLimiter.Fail(ip)
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "redirect_uri mismatch")
		return
	}
	if !verifyPKCE(verifier, ac.Challenge, ac.Method) {
		s.tokenLimiter.Fail(ip)
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "PKCE verification failed")
		return
	}
	s.tokenLimiter.Success(ip)
	s.issueTokens(w, ac.Scope)
}

func (s *Service) handleRefreshGrant(w http.ResponseWriter, r *http.Request, ip string) {
	rt := r.Form.Get("refresh_token")
	claims, err := s.parseJWT(rt)
	if err != nil || claims.TokenUse != "refresh" {
		s.tokenLimiter.Fail(ip)
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "invalid refresh token")
		return
	}
	s.tokenLimiter.Success(ip)
	s.issueTokens(w, claims.Scope)
}

func (s *Service) issueTokens(w http.ResponseWriter, scope string) {
	now := time.Now()
	access, err := s.signJWT(Claims{
		Iss: s.cfg.Issuer, Sub: s.cfg.ClientID, Aud: s.cfg.Issuer,
		Iat: now.Unix(), Exp: now.Add(s.cfg.AccessTTL).Unix(),
		Scope: scope, ClientID: s.cfg.ClientID, TokenUse: "access",
	})
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	refresh, err := s.signJWT(Claims{
		Iss: s.cfg.Issuer, Sub: s.cfg.ClientID, Aud: s.cfg.Issuer,
		Iat: now.Unix(), Exp: now.Add(s.cfg.RefreshTTL).Unix(),
		Scope: scope, ClientID: s.cfg.ClientID, TokenUse: "refresh",
	})
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  access,
		"token_type":    "Bearer",
		"expires_in":    int(s.cfg.AccessTTL.Seconds()),
		"refresh_token": refresh,
		"scope":         scope,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeOAuthError(w http.ResponseWriter, status int, code, desc string) {
	body := map[string]string{"error": code}
	if desc != "" {
		body["error_description"] = desc
	}
	writeJSON(w, status, body)
}
