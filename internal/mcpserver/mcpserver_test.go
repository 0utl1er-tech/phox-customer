package mcpserver_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/crypto"
	"github.com/0utl1er-tech/phox-customer/internal/mcpserver"
	"github.com/0utl1er-tech/phox-customer/internal/service/activity"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
	"github.com/0utl1er-tech/phox-customer/internal/service/book"
	"github.com/0utl1er-tech/phox-customer/internal/service/customer"
	"github.com/0utl1er-tech/phox-customer/internal/service/mailbox"
	"github.com/0utl1er-tech/phox-customer/internal/service/search"
	"github.com/0utl1er-tech/phox-customer/internal/testutil"
)

// ─── auth stubs ─────────────────────────────────────────────────

// stubAuth authenticates every request as the given Keycloak subject by
// injecting a minimal jwt.Token — the same context contract the real
// (*auth.Interceptor).Authenticate provides. Verifying real JWTs is the
// interceptor's own test's job (interceptor_test.go), not ours.
type stubAuth struct{ sub string }

func (s stubAuth) Authenticate(ctx context.Context, header string) (context.Context, error) {
	if header == "" {
		return nil, errors.New("authorization header is not provided")
	}
	tok := jwt.New()
	if err := tok.Set(jwt.SubjectKey, s.sub); err != nil {
		return nil, err
	}
	return context.WithValue(ctx, auth.AuthorizationPayloadKey, tok), nil
}

// ─── helpers ────────────────────────────────────────────────────

// newTestHandler builds the /mcp handler with real services on the test DB.
func newTestHandler(t *testing.T, q *db.Queries, sub string) http.Handler {
	t.Helper()
	cipher, err := crypto.NewCipherFromBase64("MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	require.NoError(t, err)
	return mcpserver.NewHandler(stubAuth{sub: sub}, mcpserver.Deps{
		Book:     book.NewBookService(q, nil),
		Customer: customer.NewCustomerService(q, nil),
		Search:   search.NewSearchService(q, nil), // ES nil → search_customers はツールエラー
		Activity: activity.NewActivityService(q, nil, nil),
		Mailbox:  mailbox.NewMailboxService(q, cipher),
	}, "")
}

// connect spins an httptest server around h and returns an initialized MCP
// client session speaking Streamable HTTP against it.
func connectClient(t *testing.T, h http.Handler) *mcp.ClientSession {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	transport := &mcp.StreamableClientTransport{
		Endpoint:   srv.URL,
		HTTPClient: &http.Client{Transport: authRoundTripper{base: http.DefaultTransport}},
		// Stateless server → server-initiated messages は来ないので
		// standalone SSE stream は張らない。
		DisableStandaloneSSE: true,
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "mcpserver-test", Version: "0"}, nil)
	session, err := client.Connect(context.Background(), transport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// authRoundTripper adds a Bearer header to every request (the stub only
// checks presence).
type authRoundTripper struct{ base http.RoundTripper }

func (a authRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer test-token")
	return a.base.RoundTrip(r)
}

// textOf extracts the single text content of a tool result.
func textOf(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	require.NotEmpty(t, res.Content)
	tc, ok := res.Content[0].(*mcp.TextContent)
	require.True(t, ok, "content[0] should be TextContent, got %T", res.Content[0])
	return tc.Text
}

// ─── tests ──────────────────────────────────────────────────────

// 401: リクエストが認証を通らなければ MCP transport まで到達しない。
func TestUnauthorized(t *testing.T) {
	// DB 不要 — サービスは呼ばれない。
	h := mcpserver.NewHandler(stubAuth{sub: "u"}, mcpserver.Deps{}, "https://example.test/.well-known/oauth-protected-resource/mcp")
	srv := httptest.NewServer(h)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader(`{}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	// Authorization ヘッダなし
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("WWW-Authenticate"), "Bearer")
	// RFC 9728: OAuth 対応クライアントの discovery 誘導
	assert.Contains(t, resp.Header.Get("WWW-Authenticate"), "resource_metadata=")
	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.NotEmpty(t, body["error"])
}

// tools/list: 公開ツール一覧が期待どおり (登録漏れ・スキーマ panic の検出)。
func TestListTools(t *testing.T) {
	// AddTool はスキーマ推論に失敗すると panic するので、handler 構築が
	// 通ること自体もこのテストの検証対象。
	h := mcpserver.NewHandler(stubAuth{sub: "u"}, mcpserver.Deps{}, "https://example.test/.well-known/oauth-protected-resource/mcp")
	session := connectClient(t, h)

	res, err := session.ListTools(context.Background(), nil)
	require.NoError(t, err)

	got := make([]string, 0, len(res.Tools))
	for _, tool := range res.Tools {
		got = append(got, tool.Name)
	}
	assert.ElementsMatch(t, []string{
		"list_books",
		"search_customers",
		"get_customer",
		"list_customer_activities",
		"list_book_activities",
		"get_call_stats",
		"get_mail_stats",
		"send_customer_email",
	}, got)
}

// list_books / get_customer / list_book_activities のハッピーパス + 認可。
// testutil.SetupTestDB は DB が無い環境では skip する (CI では postgres
// service が立つので実行される)。
func TestToolsAgainstDB(t *testing.T) {
	_, q := testutil.SetupTestDB(t)
	ctx := context.Background()

	cid := testutil.TestCompanyID(t, q)
	owner := testutil.TestUser(t, q, "mcp-owner-"+t.Name(), cid)
	outsider := testutil.TestUser(t, q, "mcp-outsider-"+t.Name(), cid)
	bk := testutil.TestBook(t, q, owner.ID)
	cust := testutil.TestCustomer(t, q, bk.ID)

	// call activity を 1 件 seed
	st, err := q.GetDefaultStatusByBookID(ctx, bk.ID)
	require.NoError(t, err)
	_, err = q.CreateActivity(ctx, db.CreateActivityParams{
		ID:         uuid.New(),
		CustomerID: cust.ID,
		Type:       "call",
		UserID:     owner.ID,
		Phone:      pgtype.Text{String: "090-1111-2222", Valid: true},
		StatusID:   pgtype.UUID{Bytes: st.ID, Valid: true},
		// zero 値 (西暦 1 年) は ListActivitiesByBookID の epoch センチネル
		// (from 未指定 = epoch 以降) より前になり除外される。now() で seed。
		OccurredAt: time.Now(),
	})
	require.NoError(t, err)

	t.Run("list_books returns the seeded book", func(t *testing.T) {
		session := connectClient(t, newTestHandler(t, q, owner.ID))
		res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_books"})
		require.NoError(t, err)
		require.False(t, res.IsError, "unexpected tool error: %s", textOf(t, res))
		assert.Contains(t, textOf(t, res), bk.ID.String())
	})

	t.Run("list_mailboxes returns a created mailbox", func(t *testing.T) {
		// owner が所有するメールボックスを 1 件作る。
		mbID := uuid.New()
		enc := []byte("enc-placeholder")
		_, err := q.CreateMailbox(ctx, db.CreateMailboxParams{
			ID: mbID, CompanyID: cid, Address: "mcp-mb-" + mbID.String() + "@0utl1er.tech",
			DisplayName: "MCP", SmtpUsername: "mcp@0utl1er.tech", PasswordEnc: enc, Active: true,
		})
		require.NoError(t, err)
		_, err = q.CreateMailboxPermit(ctx, db.CreateMailboxPermitParams{
			ID: uuid.New(), MailboxID: mbID, UserID: owner.ID, Role: db.RoleOwner,
		})
		require.NoError(t, err)

		session := connectClient(t, newTestHandler(t, q, owner.ID))
		res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_mailboxes"})
		require.NoError(t, err)
		require.False(t, res.IsError, "unexpected tool error: %s", textOf(t, res))
		assert.Contains(t, textOf(t, res), mbID.String())
	})

	t.Run("get_customer returns the customer", func(t *testing.T) {
		session := connectClient(t, newTestHandler(t, q, owner.ID))
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "get_customer",
			Arguments: map[string]any{"customer_id": cust.ID.String()},
		})
		require.NoError(t, err)
		require.False(t, res.IsError, "unexpected tool error: %s", textOf(t, res))
		assert.Contains(t, textOf(t, res), cust.ID.String())
	})

	t.Run("list_book_activities returns the seeded call", func(t *testing.T) {
		session := connectClient(t, newTestHandler(t, q, owner.ID))
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "list_book_activities",
			Arguments: map[string]any{
				"book_id": bk.ID.String(),
				"types":   []string{"call"},
			},
		})
		require.NoError(t, err)
		require.False(t, res.IsError, "unexpected tool error: %s", textOf(t, res))
		text := textOf(t, res)
		assert.Contains(t, text, "090-1111-2222")
		assert.Contains(t, text, `"totalCount"`)
	})

	t.Run("permit のないユーザーはツールエラー (PermissionDenied)", func(t *testing.T) {
		session := connectClient(t, newTestHandler(t, q, outsider.ID))
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "list_book_activities",
			Arguments: map[string]any{"book_id": bk.ID.String()},
		})
		require.NoError(t, err, "authz failure must be a tool error, not a protocol error")
		assert.True(t, res.IsError)
		assert.Contains(t, textOf(t, res), "permission_denied")
	})

	t.Run("不正な activity type はツールエラー", func(t *testing.T) {
		session := connectClient(t, newTestHandler(t, q, owner.ID))
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "list_book_activities",
			Arguments: map[string]any{
				"book_id": bk.ID.String(),
				"types":   []string{"bogus"},
			},
		})
		require.NoError(t, err)
		assert.True(t, res.IsError)
		assert.Contains(t, textOf(t, res), "unknown activity type")
	})

	t.Run("send_customer_email は SMTP 未設定だと unavailable のツールエラー", func(t *testing.T) {
		// newTestHandler は mailClient=nil で ActivityService を組むので、
		// editor 権限を持つ owner でも送信は unavailable になる。email claim
		// が無い stubAuth token では failed_precondition が先に出るため、
		// ここでは「書き込み tool が認可・前提チェックを service 層から
		// 引き継いでいる」ことをエラー種別で確認する。
		session := connectClient(t, newTestHandler(t, q, owner.ID))
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "send_customer_email",
			Arguments: map[string]any{
				"customer_id": cust.ID.String(),
				"mail_to":     "someone@example.com",
				"subject":     "test",
			},
		})
		require.NoError(t, err)
		assert.True(t, res.IsError)
		// stubAuth の token に email claim が無い → failed_precondition
		assert.Contains(t, textOf(t, res), "failed_precondition")
	})

	t.Run("permit の無いユーザーは send_customer_email できない", func(t *testing.T) {
		session := connectClient(t, newTestHandler(t, q, outsider.ID))
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "send_customer_email",
			Arguments: map[string]any{
				"customer_id": cust.ID.String(),
				"mail_to":     "someone@example.com",
				"subject":     "test",
			},
		})
		require.NoError(t, err)
		assert.True(t, res.IsError)
	})

	t.Run("search_customers は ES 未設定だと unavailable のツールエラー", func(t *testing.T) {
		session := connectClient(t, newTestHandler(t, q, owner.ID))
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "search_customers",
			Arguments: map[string]any{"query": "田中"},
		})
		require.NoError(t, err)
		assert.True(t, res.IsError)
		assert.Contains(t, textOf(t, res), "unavailable")
	})
}
