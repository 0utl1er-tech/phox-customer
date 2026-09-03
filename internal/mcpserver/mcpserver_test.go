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
	"github.com/jackc/pgx/v5/pgxpool"
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
	"github.com/0utl1er-tech/phox-customer/internal/service/campaign"
	"github.com/0utl1er-tech/phox-customer/internal/service/contact"
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
// pool は CampaignService (CreateCampaign のトランザクション) 用。nil なら
// キャンペーン系ツールは登録されない (本番の gating と同じ)。
func newTestHandler(t *testing.T, pool *pgxpool.Pool, q *db.Queries, sub string) http.Handler {
	t.Helper()
	cipher, err := crypto.NewCipherFromBase64("MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	require.NoError(t, err)
	var campaignService *campaign.CampaignService
	if pool != nil {
		// sender/cipher/tokenizer nil — SendTestEmail 以外は使わない。
		campaignService = campaign.NewCampaignService(q, pool, nil, nil, nil, "")
	}
	return mcpserver.NewHandler(stubAuth{sub: sub}, mcpserver.Deps{
		Book:     book.NewBookService(q, nil),
		Customer: customer.NewCustomerService(q, nil, nil),
		Contact:  contact.NewContactService(q),
		Search:   search.NewSearchService(q, nil), // ES nil → search_customers はツールエラー
		Activity: activity.NewActivityService(q, nil, nil),
		Mailbox:  mailbox.NewMailboxService(q, cipher, nil),
		Campaign: campaignService,
		Queries:  q,
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
		"create_customer",
	}, got)
}

// tools/list (Phase 27i): Campaign 有効時はキャンペーン系 7 ツールが載る。
// スキーマ推論 (createCampaignDraftIn のネスト構造含む) が panic しないこと
// もここで担保する。
func TestListToolsWithCampaign(t *testing.T) {
	h := mcpserver.NewHandler(stubAuth{sub: "u"}, mcpserver.Deps{
		// 登録判定 (nil チェック) にしか使わないので依存は全部 nil で良い。
		Campaign: campaign.NewCampaignService(nil, nil, nil, nil, nil, ""),
	}, "")
	session := connectClient(t, h)

	res, err := session.ListTools(context.Background(), nil)
	require.NoError(t, err)

	got := make([]string, 0, len(res.Tools))
	byName := map[string]*mcp.Tool{}
	for _, tool := range res.Tools {
		got = append(got, tool.Name)
		byName[tool.Name] = tool
	}
	for _, name := range []string{
		"list_campaigns",
		"get_campaign",
		"create_campaign_draft",
		"update_campaign_draft",
		"start_campaign",
		"pause_campaign",
		"cancel_campaign",
		"get_campaign_stats",
	} {
		assert.Contains(t, got, name)
	}
	// update_campaign_draft は部分更新であること・送信しないことを謳っている。
	require.NotNil(t, byName["update_campaign_draft"])
	assert.Contains(t, byName["update_campaign_draft"].Description, "partial update")
	assert.Contains(t, byName["update_campaign_draft"].Description, "never sends")
	// 実送信ツールには明示確認の但し書きが載っていること。
	require.NotNil(t, byName["start_campaign"])
	assert.Contains(t, byName["start_campaign"].Description, "ユーザーの明示的な確認")
	// array パラメータが null union になっていないこと (91f8dac の再発防止)。
	schema, err := json.Marshal(byName["create_campaign_draft"].InputSchema)
	require.NoError(t, err)
	assert.NotContains(t, string(schema), `"null"`)
}

// list_books / get_customer / list_book_activities のハッピーパス + 認可。
// testutil.SetupTestDB は DB が無い環境では skip する (CI では postgres
// service が立つので実行される)。
func TestToolsAgainstDB(t *testing.T) {
	pool, q := testutil.SetupTestDB(t)
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
		session := connectClient(t, newTestHandler(t, pool, q, owner.ID))
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

		session := connectClient(t, newTestHandler(t, pool, q, owner.ID))
		res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_mailboxes"})
		require.NoError(t, err)
		require.False(t, res.IsError, "unexpected tool error: %s", textOf(t, res))
		assert.Contains(t, textOf(t, res), mbID.String())

		// Phase 26: メッセージを 1 件 seed して list → get の順で読む。
		msgID := uuid.New()
		_, err = q.CreateMailboxMessage(ctx, db.CreateMailboxMessageParams{
			ID: msgID, MailboxID: mbID, Folder: "INBOX",
			MessageID: "mcp-test-" + msgID.String(), FromAddr: "prospect@example.com",
			ToAddrs: "mcp@0utl1er.tech", Subject: "新規のお問い合わせ",
			BodyText: "御社サービスについて詳しく知りたいです。", OccurredAt: time.Now(),
		})
		require.NoError(t, err)

		res, err = session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "list_mailbox_messages",
			Arguments: map[string]any{"mailbox_id": mbID.String()},
		})
		require.NoError(t, err)
		require.False(t, res.IsError, "unexpected tool error: %s", textOf(t, res))
		listText := textOf(t, res)
		assert.Contains(t, listText, "prospect@example.com")
		assert.NotContains(t, listText, "詳しく知りたい", "list は本文を返さない")

		res, err = session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "get_mailbox_message",
			Arguments: map[string]any{"message_id": msgID.String()},
		})
		require.NoError(t, err)
		require.False(t, res.IsError, "unexpected tool error: %s", textOf(t, res))
		assert.Contains(t, textOf(t, res), "詳しく知りたい", "get は本文を返す")

		// permit の無い outsider には見えない。
		xsession := connectClient(t, newTestHandler(t, pool, q, outsider.ID))
		res, err = xsession.CallTool(ctx, &mcp.CallToolParams{
			Name:      "list_mailbox_messages",
			Arguments: map[string]any{"mailbox_id": mbID.String()},
		})
		require.NoError(t, err)
		assert.True(t, res.IsError, "permit なしは permission_denied になるべき")
	})

	t.Run("create_customer creates then upserts by mail", func(t *testing.T) {
		session := connectClient(t, newTestHandler(t, pool, q, owner.ID))
		mail := "inquiry-" + uuid.NewString() + "@example.com"

		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "create_customer",
			Arguments: map[string]any{
				"book_id": bk.ID.String(), "name": "問い合わせ 太郎", "mail": mail,
				"memo": "メールからの新規問い合わせ",
			},
		})
		require.NoError(t, err)
		require.False(t, res.IsError, "unexpected tool error: %s", textOf(t, res))
		first := textOf(t, res)

		// 同じ mail でもう一度 → 新規作成ではなく既存を返す (id が同じ)。
		res, err = session.CallTool(ctx, &mcp.CallToolParams{
			Name: "create_customer",
			Arguments: map[string]any{
				"book_id": bk.ID.String(), "name": "別名で再作成しようとする", "mail": mail,
			},
		})
		require.NoError(t, err)
		require.False(t, res.IsError, "unexpected tool error: %s", textOf(t, res))
		second := textOf(t, res)
		assert.Contains(t, second, "問い合わせ 太郎", "既存顧客がそのまま返るべき (上書きしない)")

		var f1, f2 struct {
			Customer struct {
				Id string `json:"id"`
			} `json:"customer"`
		}
		require.NoError(t, json.Unmarshal([]byte(first), &f1))
		require.NoError(t, json.Unmarshal([]byte(second), &f2))
		assert.Equal(t, f1.Customer.Id, f2.Customer.Id, "同一 mail は同一顧客に upsert")

		// editor 権限の無い outsider は作れない。
		xsession := connectClient(t, newTestHandler(t, pool, q, outsider.ID))
		res, err = xsession.CallTool(ctx, &mcp.CallToolParams{
			Name: "create_customer",
			Arguments: map[string]any{
				"book_id": bk.ID.String(), "name": "無権限", "mail": "x-" + uuid.NewString() + "@example.com",
			},
		})
		require.NoError(t, err)
		assert.True(t, res.IsError, "editor なしは permission_denied になるべき")
	})

	t.Run("create_customer with contacts links contact-email history", func(t *testing.T) {
		// メールボックスに、contact のアドレスから来た未紐付けメールを seed。
		mbID := uuid.New()
		_, err := q.CreateMailbox(ctx, db.CreateMailboxParams{
			ID: mbID, CompanyID: cid, Address: "cmb-" + mbID.String() + "@0utl1er.tech",
			SmtpUsername: "cmb@0utl1er.tech", PasswordEnc: []byte("x"), Active: true,
		})
		require.NoError(t, err)
		contactMail := "kako-" + uuid.NewString()[:8] + "@levtech.jp"
		cmsgID := "<" + uuid.NewString() + "@levtech.jp>"
		_, err = q.CreateMailboxMessage(ctx, db.CreateMailboxMessageParams{
			ID: uuid.New(), MailboxID: mbID, Folder: "INBOX", MessageID: cmsgID,
			FromAddr: contactMail, ToAddrs: "cmb@0utl1er.tech", Subject: "担当者からの連絡",
			BodyText: "よろしくお願いします", OccurredAt: time.Now().Add(-time.Hour),
		})
		require.NoError(t, err)

		session := connectClient(t, newTestHandler(t, pool, q, owner.ID))
		custMail := "levtech-main-" + uuid.NewString() + "@levtech.jp"
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "create_customer",
			Arguments: map[string]any{
				"book_id": bk.ID.String(), "name": "レバテック", "mail": custMail,
				"contacts": []any{
					map[string]any{"name": "加古", "mail": contactMail, "phone": "03-1111-2222"},
				},
			},
		})
		require.NoError(t, err)
		require.False(t, res.IsError, "unexpected tool error: %s", textOf(t, res))

		var out struct {
			Customer struct {
				Id string `json:"id"`
			} `json:"customer"`
		}
		require.NoError(t, json.Unmarshal([]byte(textOf(t, res)), &out))
		custID, _ := uuid.Parse(out.Customer.Id)

		// contact が作られている。
		contacts, err := q.ListContacts(ctx, custID)
		require.NoError(t, err)
		require.Len(t, contacts, 1)
		assert.Equal(t, contactMail, contacts[0].Mail)

		// contact のアドレスのメールが Activity 化され、contact_id が付く。
		act, err := q.GetActivityByMessageID(ctx, pgtype.Text{String: cmsgID, Valid: true})
		require.NoError(t, err, "contact のメールが顧客タイムラインに載るべき")
		assert.Equal(t, custID, act.CustomerID)
		require.True(t, act.ContactID.Valid, "contact_id が付くべき")
		assert.Equal(t, contacts[0].ID, uuid.UUID(act.ContactID.Bytes))

		// 冪等: 同じ contacts でもう一度 → contact も Activity も増えない。
		res, err = session.CallTool(ctx, &mcp.CallToolParams{
			Name: "create_customer",
			Arguments: map[string]any{
				"book_id": bk.ID.String(), "name": "レバテック", "mail": custMail,
				"contacts": []any{map[string]any{"name": "加古", "mail": contactMail}},
			},
		})
		require.NoError(t, err)
		require.False(t, res.IsError)
		contacts2, _ := q.ListContacts(ctx, custID)
		assert.Len(t, contacts2, 1, "同一 mail の contact は重複作成しない")
	})

	t.Run("get_customer returns the customer", func(t *testing.T) {
		session := connectClient(t, newTestHandler(t, pool, q, owner.ID))
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "get_customer",
			Arguments: map[string]any{"customer_id": cust.ID.String()},
		})
		require.NoError(t, err)
		require.False(t, res.IsError, "unexpected tool error: %s", textOf(t, res))
		assert.Contains(t, textOf(t, res), cust.ID.String())
	})

	t.Run("list_book_activities returns the seeded call", func(t *testing.T) {
		session := connectClient(t, newTestHandler(t, pool, q, owner.ID))
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
		session := connectClient(t, newTestHandler(t, pool, q, outsider.ID))
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "list_book_activities",
			Arguments: map[string]any{"book_id": bk.ID.String()},
		})
		require.NoError(t, err, "authz failure must be a tool error, not a protocol error")
		assert.True(t, res.IsError)
		assert.Contains(t, textOf(t, res), "permission_denied")
	})

	t.Run("不正な activity type はツールエラー", func(t *testing.T) {
		session := connectClient(t, newTestHandler(t, pool, q, owner.ID))
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
		session := connectClient(t, newTestHandler(t, pool, q, owner.ID))
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
		session := connectClient(t, newTestHandler(t, pool, q, outsider.ID))
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
		session := connectClient(t, newTestHandler(t, pool, q, owner.ID))
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "search_customers",
			Arguments: map[string]any{"query": "田中"},
		})
		require.NoError(t, err)
		assert.True(t, res.IsError)
		assert.Contains(t, textOf(t, res), "unavailable")
	})

	t.Run("campaign draft lifecycle (Phase 27i)", func(t *testing.T) {
		// MX チェックが CI の DNS 事情に左右されないよう、受信者ドメインの
		// 判定結果をキャッシュに seed しておく (Check はキャッシュ優先)。
		require.NoError(t, q.UpsertDomainHealth(ctx, db.UpsertDomainHealthParams{
			Lower: "example.com", HasMx: true, MxHost: "mx.example.com",
		}))

		// 送信プール mailbox (owner が editor 以上)。
		mbID := uuid.New()
		_, err := q.CreateMailbox(ctx, db.CreateMailboxParams{
			ID: mbID, CompanyID: cid, Address: "camp-" + mbID.String()[:8] + "@0utl1er.tech",
			SmtpUsername: "camp@0utl1er.tech", PasswordEnc: []byte("x"), Active: true,
		})
		require.NoError(t, err)
		_, err = q.CreateMailboxPermit(ctx, db.CreateMailboxPermitParams{
			ID: uuid.New(), MailboxID: mbID, UserID: owner.ID, Role: db.RoleOwner,
		})
		require.NoError(t, err)

		session := connectClient(t, newTestHandler(t, pool, q, owner.ID))

		// ── create_campaign_draft: draft で作られ、送信はされない ──
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "create_campaign_draft",
			Arguments: map[string]any{
				"name":         "mcp-test-campaign",
				"customer_ids": []string{cust.ID.String()},
				"mailbox_ids":  []string{mbID.String()},
				"subject":      "{{customer_name}} 様へのご案内",
				"body":         "本文です。",
				"followups": []any{
					map[string]any{"wait_days": 3, "body": "その後いかがでしょうか。"},
				},
			},
		})
		require.NoError(t, err)
		require.False(t, res.IsError, "unexpected tool error: %s", textOf(t, res))
		draftJSON := textOf(t, res)
		assert.Contains(t, draftJSON, `"status":"draft"`)
		assert.Contains(t, draftJSON, `"queuedCount":1`)
		// 2 要素目に「start_campaign を明示的に」の注意文が付く。
		require.Len(t, res.Content, 2)
		notice, ok := res.Content[1].(*mcp.TextContent)
		require.True(t, ok)
		assert.Contains(t, notice.Text, "start_campaign")
		assert.Contains(t, notice.Text, "送信されていません")

		var created struct {
			Campaign struct {
				Id string `json:"id"`
			} `json:"campaign"`
		}
		require.NoError(t, json.Unmarshal([]byte(draftJSON), &created))
		campID := created.Campaign.Id
		require.NotEmpty(t, campID)

		// ── get_campaign: followups 込みで読める ──
		res, err = session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "get_campaign",
			Arguments: map[string]any{"campaign_id": campID},
		})
		require.NoError(t, err)
		require.False(t, res.IsError, "unexpected tool error: %s", textOf(t, res))
		gotCampaign := textOf(t, res)
		assert.Contains(t, gotCampaign, `"status":"draft"`)
		assert.Contains(t, gotCampaign, "その後いかがでしょうか")

		// ── list_campaigns: 一覧に載る ──
		res, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "list_campaigns"})
		require.NoError(t, err)
		require.False(t, res.IsError, "unexpected tool error: %s", textOf(t, res))
		assert.Contains(t, textOf(t, res), campID)

		// ── get_campaign_stats: stats + timeseries が返る ──
		res, err = session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "get_campaign_stats",
			Arguments: map[string]any{"campaign_id": campID},
		})
		require.NoError(t, err)
		require.False(t, res.IsError, "unexpected tool error: %s", textOf(t, res))
		statsJSON := textOf(t, res)
		assert.Contains(t, statsJSON, `"stats"`)
		assert.Contains(t, statsJSON, `"timeseries"`)
		assert.Contains(t, statsJSON, `"queued":1`)

		// ── start_campaign: 特電法の送信者表示が空なので失敗する ──
		res, err = session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "start_campaign",
			Arguments: map[string]any{"campaign_id": campID},
		})
		require.NoError(t, err)
		assert.True(t, res.IsError, "sender 未設定の開始は failed_precondition になるべき")
		assert.Contains(t, textOf(t, res), "特定電子メール法")

		// ── pause_campaign: draft は一時停止できない ──
		res, err = session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "pause_campaign",
			Arguments: map[string]any{"campaign_id": campID},
		})
		require.NoError(t, err)
		assert.True(t, res.IsError)

		// ── mailbox permit の無い outsider は draft を作れない ──
		xsession := connectClient(t, newTestHandler(t, pool, q, outsider.ID))
		res, err = xsession.CallTool(ctx, &mcp.CallToolParams{
			Name: "create_campaign_draft",
			Arguments: map[string]any{
				"name":         "no-permission",
				"customer_ids": []string{cust.ID.String()},
				"mailbox_ids":  []string{mbID.String()},
				"subject":      "x",
				"body":         "x",
			},
		})
		require.NoError(t, err)
		assert.True(t, res.IsError, "mailbox editor なしは permission_denied になるべき")

		// ── cancel_campaign: 後始末 (draft → cancelled) ──
		res, err = session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "cancel_campaign",
			Arguments: map[string]any{"campaign_id": campID},
		})
		require.NoError(t, err)
		require.False(t, res.IsError, "unexpected tool error: %s", textOf(t, res))
		assert.Contains(t, textOf(t, res), `"status":"cancelled"`)
	})

	t.Run("update_campaign_draft (partial update)", func(t *testing.T) {
		require.NoError(t, q.UpsertDomainHealth(ctx, db.UpsertDomainHealthParams{
			Lower: "example.com", HasMx: true, MxHost: "mx.example.com",
		}))
		mbID := uuid.New()
		_, err := q.CreateMailbox(ctx, db.CreateMailboxParams{
			ID: mbID, CompanyID: cid, Address: "upd-" + mbID.String()[:8] + "@0utl1er.tech",
			SmtpUsername: "upd@0utl1er.tech", PasswordEnc: []byte("x"), Active: true,
		})
		require.NoError(t, err)
		_, err = q.CreateMailboxPermit(ctx, db.CreateMailboxPermitParams{
			ID: uuid.New(), MailboxID: mbID, UserID: owner.ID, Role: db.RoleOwner,
		})
		require.NoError(t, err)

		session := connectClient(t, newTestHandler(t, pool, q, owner.ID))
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "create_campaign_draft",
			Arguments: map[string]any{
				"name":         "mcp-update-target",
				"customer_ids": []string{cust.ID.String()},
				"mailbox_ids":  []string{mbID.String()},
				"subject":      "初版の件名",
				"body":         "初版の本文です。",
				"followups": []any{
					map[string]any{"wait_days": 3, "body": "追いメール初版"},
				},
			},
		})
		require.NoError(t, err)
		require.False(t, res.IsError, "unexpected tool error: %s", textOf(t, res))
		var created struct {
			Campaign struct {
				Id string `json:"id"`
			} `json:"campaign"`
		}
		require.NoError(t, json.Unmarshal([]byte(textOf(t, res)), &created))
		campID := created.Campaign.Id

		// ── 部分更新: subject だけ変更 → name/body/followups は不変 ──
		res, err = session.CallTool(ctx, &mcp.CallToolParams{
			Name: "update_campaign_draft",
			Arguments: map[string]any{
				"campaign_id": campID,
				"subject":     "改訂版の件名",
			},
		})
		require.NoError(t, err)
		require.False(t, res.IsError, "unexpected tool error: %s", textOf(t, res))
		updated := textOf(t, res)
		assert.Contains(t, updated, "改訂版の件名")
		assert.Contains(t, updated, "mcp-update-target", "name は不変のはず")
		assert.Contains(t, updated, "初版の本文です", "body は不変のはず")
		assert.Contains(t, updated, "追いメール初版", "followups は不変のはず")
		assert.Contains(t, updated, `"status":"draft"`, "更新しても draft のまま")

		// ── followups の全置換: wait_days 3→7 + 2 ステップ目追加 ──
		res, err = session.CallTool(ctx, &mcp.CallToolParams{
			Name: "update_campaign_draft",
			Arguments: map[string]any{
				"campaign_id": campID,
				"followups": []any{
					map[string]any{"wait_days": 7, "body": "追いメール改訂版"},
					map[string]any{"wait_days": 14, "body": "最終確認のご連絡"},
				},
			},
		})
		require.NoError(t, err)
		require.False(t, res.IsError, "unexpected tool error: %s", textOf(t, res))
		updated = textOf(t, res)
		assert.Contains(t, updated, `"waitDays":7`)
		assert.Contains(t, updated, "最終確認のご連絡")
		assert.NotContains(t, updated, "追いメール初版", "followups は全置換")
		assert.Contains(t, updated, "改訂版の件名", "前回の subject 変更は保持")

		// ── schedule の部分更新: send_start_hour だけ → 他の pacing は不変 ──
		res, err = session.CallTool(ctx, &mcp.CallToolParams{
			Name: "update_campaign_draft",
			Arguments: map[string]any{
				"campaign_id": campID,
				"schedule":    map[string]any{"send_start_hour": 10},
			},
		})
		require.NoError(t, err)
		require.False(t, res.IsError, "unexpected tool error: %s", textOf(t, res))
		updated = textOf(t, res)
		assert.Contains(t, updated, `"sendStartHour":10`)
		assert.Contains(t, updated, `"dailyCapPerMailbox":100`, "未指定の schedule フィールドは現在値のまま")
		assert.Contains(t, updated, `"sendEndHour":18`, "未指定の schedule フィールドは現在値のまま")

		// ── followups の検証は create と同じガード ──
		res, err = session.CallTool(ctx, &mcp.CallToolParams{
			Name: "update_campaign_draft",
			Arguments: map[string]any{
				"campaign_id": campID,
				"followups":   []any{map[string]any{"wait_days": 61, "body": "x"}},
			},
		})
		require.NoError(t, err)
		assert.True(t, res.IsError)
		assert.Contains(t, textOf(t, res), "wait_days")

		// ── mailbox permit の無い outsider は更新できない ──
		xsession := connectClient(t, newTestHandler(t, pool, q, outsider.ID))
		res, err = xsession.CallTool(ctx, &mcp.CallToolParams{
			Name: "update_campaign_draft",
			Arguments: map[string]any{
				"campaign_id": campID,
				"subject":     "乗っ取り",
			},
		})
		require.NoError(t, err)
		assert.True(t, res.IsError, "権限なしは permission_denied になるべき")

		// ── cancelled は編集不可 (failed_precondition) ──
		res, err = session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "cancel_campaign",
			Arguments: map[string]any{"campaign_id": campID},
		})
		require.NoError(t, err)
		require.False(t, res.IsError, "unexpected tool error: %s", textOf(t, res))
		res, err = session.CallTool(ctx, &mcp.CallToolParams{
			Name: "update_campaign_draft",
			Arguments: map[string]any{
				"campaign_id": campID,
				"subject":     "もう遅い",
			},
		})
		require.NoError(t, err)
		assert.True(t, res.IsError, "cancelled の編集は failed_precondition になるべき")
		assert.Contains(t, textOf(t, res), "failed_precondition")
	})
}
