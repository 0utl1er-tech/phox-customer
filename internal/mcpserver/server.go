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
	"net/http"

	"connectrpc.com/connect"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/0utl1er-tech/phox-customer/internal/service/activity"
	"github.com/0utl1er-tech/phox-customer/internal/service/book"
	"github.com/0utl1er-tech/phox-customer/internal/service/customer"
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
	Search   *search.SearchService
	Activity *activity.ActivityService
}

// NewHandler builds the authenticated /mcp http.Handler.
func NewHandler(authn Authenticator, deps Deps) http.Handler {
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

	return requireBearer(authn, streamable)
}

// requireBearer authenticates every request with the shared Keycloak JWT
// path before it reaches the MCP transport. On failure it replies 401 with
// a JSON body (MCP clients surface the message).
func requireBearer(authn Authenticator, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, err := authn.Authenticate(r.Context(), r.Header.Get("Authorization"))
		if err != nil {
			msg := "unauthenticated"
			var cerr *connect.Error
			if errors.As(err, &cerr) {
				msg = cerr.Message()
			}
			w.Header().Set("WWW-Authenticate", `Bearer realm="phox-crm"`)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
			return
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
