// Package mcpserver exposes a read-only subset of the Phox CRM as an MCP
// (Model Context Protocol) server over the Streamable HTTP transport,
// mounted at /mcp on the existing Connect-Go mux.
//
// Design notes:
//
//   - Auth reuses the exact same path as every Connect RPC: the middleware
//     calls (*auth.Interceptor).Authenticate, which verifies the Keycloak JWT
//     (issuer / audience / signature via JWKS), JIT-provisions the User row,
//     and stores the token under auth.AuthorizationPayloadKey in the request
//     context. Tool handlers then call the existing service structs
//     *in-process*, so every Permit / role check those services perform
//     applies to MCP calls unchanged.
//
//   - The transport runs in Stateless mode: no Mcp-Session-Id bookkeeping,
//     each POST is self-contained. That keeps the endpoint safe behind the
//     gateway with replicas > 1 (no session affinity) and — critically —
//     means the per-request context (with the caller's JWT) reaches the tool
//     handler; a stateful session would pin the context of whichever request
//     opened the session.
//
//   - v1 is read-only (list/search/get/stats). Write tools (e.g.
//     create_activity_call) are deliberately excluded until there's a
//     concrete workflow that needs them.
package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"connectrpc.com/connect"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/service/activity"
	"github.com/0utl1er-tech/phox-customer/internal/service/book"
	"github.com/0utl1er-tech/phox-customer/internal/service/contact"
	"github.com/0utl1er-tech/phox-customer/internal/service/customer"
	"github.com/0utl1er-tech/phox-customer/internal/service/mailbox"
	"github.com/0utl1er-tech/phox-customer/internal/service/search"
)

// serverName / serverVersion identify this MCP server to clients
// (initialize → serverInfo).
const (
	serverName    = "phox-crm"
	serverVersion = "0.1.0"
)

// Authenticator is the slice of *auth.Interceptor the MCP middleware needs.
// Declared as an interface so tests can substitute a stub without spinning
// up a JWKS endpoint.
type Authenticator interface {
	// Authenticate validates an Authorization header value and returns a
	// context carrying the verified token (auth.AuthorizationPayloadKey).
	Authenticate(ctx context.Context, authorizationHeader string) (context.Context, error)
}

// Deps carries the already-constructed Phox services the tools delegate to.
// All of them enforce authorization internally from the request context.
type Deps struct {
	Book     *book.BookService
	Customer *customer.CustomerService
	Contact  *contact.ContactService
	Search   *search.SearchService
	Activity *activity.ActivityService
	Mailbox  *mailbox.MailboxService // nil 可 (MAILBOX_SECRET_KEY 未設定時)
	// Queries は create_customer の upsert 判定 (book 内 mail 一致検索) 専用。
	// 結果は必ず authz 付きのサービス RPC を通して返すこと (生データを直接
	// クライアントへ返さない)。nil 可 (その場合 upsert 判定はスキップ)。
	Queries *db.Queries
}

// ProtectedResourceMetadata is the RFC 9728 document served at
// /.well-known/oauth-protected-resource(/mcp). OAuth-capable MCP clients
// (Claude Code など) はこれを読んで authorization server (Keycloak) を発見し、
// 認可コードフロー + refresh token でトークンを自動更新する — 静的 Bearer
// ヘッダ運用だと 40 分で失効して "404 page not found" (discovery 先が無い)
// で死ぬ問題 (実証 2026-07-03) の恒久解。
type ProtectedResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	BearerMethodsSupported []string `json:"bearer_methods_supported"`
	ScopesSupported        []string `json:"scopes_supported"`
}

// ProtectedResourceMetadataHandler serves the RFC 9728 metadata JSON.
// resourceURL は公開 MCP エンドポイント (例 https://.../mcp)、issuerURL は
// Keycloak realm issuer (cfg.JWTIssuerURL と同一値)。
func ProtectedResourceMetadataHandler(resourceURL, issuerURL string) http.Handler {
	meta := ProtectedResourceMetadata{
		Resource:               resourceURL,
		AuthorizationServers:   []string{issuerURL},
		BearerMethodsSupported: []string{"header"},
		// offline_access: これが無いと refresh token が Keycloak の SSO セッション
		// (idle 30 分) に紐付き、放置後の自動更新が "Token is not active" で恒久
		// 失敗する (実証 2026-07-09)。offline token 化で idle 30 日に伸ばす。
		ScopesSupported: []string{"openid", "profile", "email", "offline_access"},
	}
	body, err := json.Marshal(meta)
	if err != nil {
		panic(fmt.Sprintf("mcpserver: marshal protected resource metadata: %v", err))
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_, _ = w.Write(body)
	})
}

// NewHandler builds the authenticated /mcp http.Handler.
//
// resourceMetadataURL (optional, "" で省略) は 401 応答の WWW-Authenticate に
// RFC 9728 の resource_metadata パラメータとして載せ、クライアントの OAuth
// discovery を誘導する。
func NewHandler(authn Authenticator, deps Deps, resourceMetadataURL string) http.Handler {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    serverName,
		Title:   "Phox CRM",
		Version: serverVersion,
	}, nil)

	addTools(server, deps)

	streamable := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{
			// See package doc: stateless keeps per-request auth context intact
			// and avoids session affinity behind the gateway.
			Stateless: true,
			// Plain JSON responses (not SSE) — curl / API-test friendly, and
			// nothing here streams.
			JSONResponse: true,
		},
	)

	return requireBearer(authn, resourceMetadataURL, streamable)
}

// requireBearer authenticates every request with the shared Keycloak JWT
// path before it reaches the MCP transport. On failure it replies 401 with
// a JSON body (MCP clients surface the message) and a WWW-Authenticate
// header carrying the RFC 9728 resource_metadata pointer when configured.
func requireBearer(authn Authenticator, resourceMetadataURL string, next http.Handler) http.Handler {
	challenge := `Bearer realm="phox-crm"`
	if resourceMetadataURL != "" {
		challenge = fmt.Sprintf(`Bearer realm="phox-crm", resource_metadata=%q`, resourceMetadataURL)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, err := authn.Authenticate(r.Context(), r.Header.Get("Authorization"))
		if err != nil {
			msg := "unauthenticated"
			var cerr *connect.Error
			if errors.As(err, &cerr) {
				msg = cerr.Message()
			}
			w.Header().Set("WWW-Authenticate", challenge)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
			return
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
