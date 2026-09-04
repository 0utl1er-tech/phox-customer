package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"

	activityv1 "github.com/0utl1er-tech/phox-customer/gen/pb/activity/v1"
	bookv1 "github.com/0utl1er-tech/phox-customer/gen/pb/book/v1"
	campaignv1 "github.com/0utl1er-tech/phox-customer/gen/pb/campaign/v1"
	contactv1 "github.com/0utl1er-tech/phox-customer/gen/pb/contact/v1"
	customerv1 "github.com/0utl1er-tech/phox-customer/gen/pb/customer/v1"
	mailboxv1 "github.com/0utl1er-tech/phox-customer/gen/pb/mailbox/v1"
	searchv1 "github.com/0utl1er-tech/phox-customer/gen/pb/search/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
	"github.com/0utl1er-tech/phox-customer/internal/service/customer"
)

// ─── input types ────────────────────────────────────────────────
//
// jsonschema struct tags become property descriptions in the generated tool
// input schema (SDK infers the schema from the Go type).

type emptyIn struct{}

type searchCustomersIn struct {
	Query      string   `json:"query,omitempty" jsonschema:"free-text search query (Japanese full-text via kuromoji); empty string browses without a text constraint"`
	Prefecture string   `json:"prefecture,omitempty" jsonschema:"prefecture keyword filter, e.g. 東京都; empty = no filter"`
	BookIDs    []string `json:"book_ids,omitempty" jsonschema:"restrict to these book UUIDs; empty = all books you can access"`
	Limit      int32    `json:"limit,omitempty" jsonschema:"max hits to return (server clamps to 100); 0 = server default"`
	Offset     int32    `json:"offset,omitempty" jsonschema:"pagination offset"`
}

type getCustomerIn struct {
	CustomerID string `json:"customer_id" jsonschema:"customer UUID"`
}

type listCustomerActivitiesIn struct {
	CustomerID string   `json:"customer_id" jsonschema:"customer UUID"`
	Types      []string `json:"types,omitempty" jsonschema:"filter by activity type: call | email_sent | email_received; empty = all types"`
}

type listBookActivitiesIn struct {
	BookID       string   `json:"book_id" jsonschema:"book UUID"`
	Types        []string `json:"types,omitempty" jsonschema:"filter by activity type: call | email_sent | email_received; empty = all types"`
	UserID       string   `json:"user_id,omitempty" jsonschema:"filter by assignee user id (Keycloak sub); empty = all users"`
	OccurredFrom string   `json:"occurred_from,omitempty" jsonschema:"inclusive lower bound, RFC3339 (e.g. 2026-06-01T00:00:00+09:00); empty = unbounded"`
	OccurredTo   string   `json:"occurred_to,omitempty" jsonschema:"exclusive upper bound, RFC3339; empty = unbounded"`
	Limit        int32    `json:"limit,omitempty" jsonschema:"page size (server default 50, max 200)"`
	Offset       int32    `json:"offset,omitempty" jsonschema:"pagination offset"`
}

type statsIn struct {
	BookID       string `json:"book_id" jsonschema:"book UUID"`
	OccurredFrom string `json:"occurred_from,omitempty" jsonschema:"inclusive lower bound, RFC3339; empty = unbounded"`
	OccurredTo   string `json:"occurred_to,omitempty" jsonschema:"exclusive upper bound, RFC3339; empty = unbounded"`
}

type listMailboxMessagesIn struct {
	MailboxID string `json:"mailbox_id" jsonschema:"mailbox UUID (from list_mailboxes)"`
	Folder    string `json:"folder,omitempty" jsonschema:"'INBOX' (received) or 'Sent'; omit for both"`
	Limit     int32  `json:"limit,omitempty" jsonschema:"max messages to return (1-200, default 50)"`
	Offset    int32  `json:"offset,omitempty" jsonschema:"pagination offset"`
}

type getMailboxMessageIn struct {
	MessageID string `json:"message_id" jsonschema:"MailboxMessage UUID (the 'id' field from list_mailbox_messages, NOT the RFC822 message_id)"`
}

type createCustomerContactIn struct {
	Mail  string `json:"mail" jsonschema:"contact email address — past and future mailbox messages from/to this address are linked to the customer's timeline"`
	Name  string `json:"name,omitempty" jsonschema:"contact person name"`
	Phone string `json:"phone,omitempty" jsonschema:"contact phone number"`
}

type createCustomerIn struct {
	BookID      string `json:"book_id" jsonschema:"book UUID to add the customer to (requires editor role)"`
	Name        string `json:"name" jsonschema:"customer (person) name"`
	Mail        string `json:"mail,omitempty" jsonschema:"email address — if a customer with this mail already exists in the book, that customer is returned instead of creating a duplicate"`
	Phone       string `json:"phone,omitempty" jsonschema:"phone number"`
	Corporation string `json:"corporation,omitempty" jsonschema:"company/organisation name"`
	Category    string `json:"category,omitempty" jsonschema:"business category"`
	Address     string `json:"address,omitempty" jsonschema:"postal address"`
	Memo        string `json:"memo,omitempty" jsonschema:"free-form memo, e.g. summary of the inquiry email this customer was created from"`
	// 同一取引先が複数アドレスを持つ場合 (例: 会社の担当者ごとのメール) に、
	// それぞれを contact として登録し履歴を顧客に集約する。冪等 (同一 mail は再作成しない)。
	Contacts []createCustomerContactIn `json:"contacts,omitempty" jsonschema:"additional contacts (email addresses) belonging to this customer, e.g. multiple people/addresses at the same company; each becomes a contact and its mailbox history is linked to the customer"`
	// Phase 29b: 顧客ごとの任意差し込み変数。キャンペーン本文の {{fields.<key>}}。
	CustomFields map[string]string `json:"custom_fields,omitempty" jsonschema:"per-customer merge variables usable in campaign bodies as {{fields.<key>}} (e.g. {\"meo_score\":\"30/100\"} → {{fields.meo_score}}). Keys are lowercased and non [a-z0-9_] characters become '_'; max 32 keys, 4096 bytes per value. Values are plain text and may contain newlines (rendered as <br> in the HTML part)"`
}

type sendCustomerEmailIn struct {
	CustomerID string `json:"customer_id" jsonschema:"customer UUID — the email is recorded as an activity on this customer's timeline"`
	MailTo     string `json:"mail_to" jsonschema:"recipient email address"`
	MailCc     string `json:"mail_cc,omitempty" jsonschema:"optional CC address"`
	Subject    string `json:"subject" jsonschema:"mail subject (required, min 1 char)"`
	Body       string `json:"body,omitempty" jsonschema:"plain-text mail body"`
	ContactID  string `json:"contact_id,omitempty" jsonschema:"optional contact UUID to associate the mail with"`
	MailboxID  string `json:"mailbox_id,omitempty" jsonschema:"optional mailbox UUID (from list_mailboxes) to send as — the From address becomes that mailbox and replies flow back to it; requires editor role on the mailbox. Omit for the legacy send-as-yourself behaviour"`
}

type importCustomersCSVIn struct {
	BookName   string `json:"book_name" jsonschema:"name for the NEW customer book that is created to hold the imported rows"`
	CSVContent string `json:"csv_content" jsonschema:"raw CSV text, first line = header row. Canonical headers (case-insensitive): name, corporation, phone, mail (or email), category, address, memo, id (optional customer UUID). ANY OTHER COLUMN is stored as a per-customer merge variable and can be used in campaign bodies as {{fields.<column>}} — e.g. a meo_score column becomes {{fields.meo_score}}. Column names are lowercased and non [a-z0-9_] characters become '_'; max 32 such columns, 4096 bytes per cell"`
}

type enrichCustomerRowsIn struct {
	Headers      []string   `json:"headers" jsonschema:"the raw CSV column names as-is (Japanese or arbitrary labels are fine), 1-64 columns"`
	Rows         [][]string `json:"rows" jsonschema:"raw row data: one array of cell values per row, in the same order as headers (max 500 rows per call — split larger files)"`
	TargetFields []string   `json:"target_fields,omitempty" jsonschema:"restrict which canonical fields to fill: phone | category | name | corporation | address | memo | mail; empty = all"`
}

// ─── campaign input types (Phase 27i) ───────────────────────────

type listCampaignsIn struct {
	Limit  int32 `json:"limit,omitempty" jsonschema:"page size (server default 50, max 100)"`
	Offset int32 `json:"offset,omitempty" jsonschema:"pagination offset"`
}

type campaignIDIn struct {
	CampaignID string `json:"campaign_id" jsonschema:"campaign UUID (from list_campaigns or create_campaign_draft)"`
}

type campaignFollowupIn struct {
	WaitDays int32  `json:"wait_days" jsonschema:"days to wait after the previous step (1-60); the followup is skipped automatically once the recipient replies"`
	Subject  string `json:"subject,omitempty" jsonschema:"followup subject; empty = 'Re: <first subject>' so it threads as a reply"`
	Body     string `json:"body" jsonschema:"followup plain-text body (required); same placeholders as the first body"`
}

// campaignScheduleIn — 全フィールド省略可。省略したフィールドはサービス既定値
// (9-18 時 JST / 平日 / 100 通/日/mailbox / 90 秒間隔 / warmup on / bounce 5%)
// に落ちる。pointer なのは bool/0 と「未指定」を区別するため。
type campaignScheduleIn struct {
	SendStartHour        *int32 `json:"send_start_hour,omitempty" jsonschema:"JST hour to start sending (0-23); default 9"`
	SendEndHour          *int32 `json:"send_end_hour,omitempty" jsonschema:"JST hour to stop sending (1-24, must be after send_start_hour); default 18"`
	SendDays             *int32 `json:"send_days,omitempty" jsonschema:"weekday bitmask Mon=1 Tue=2 Wed=4 Thu=8 Fri=16 Sat=32 Sun=64; default 31 (weekdays)"`
	DailyCapPerMailbox   *int32 `json:"daily_cap_per_mailbox,omitempty" jsonschema:"max mails per mailbox per day (1-1000); default 100"`
	MinIntervalSec       *int32 `json:"min_interval_sec,omitempty" jsonschema:"min seconds between two sends from the same mailbox (10-3600); default 90"`
	WarmupEnabled        *bool  `json:"warmup_enabled,omitempty" jsonschema:"ramp up daily volume gradually for fresh mailboxes; default true"`
	BouncePauseThreshold *int32 `json:"bounce_pause_threshold,omitempty" jsonschema:"auto-pause the campaign when hard-bounce rate (%) exceeds this; 0 disables; default 5"`
}

func (in *campaignScheduleIn) toProto() *campaignv1.CampaignSchedule {
	// 既定値は create_campaign.go の schedule 未指定時デフォルトと同一。
	return in.apply(&campaignv1.CampaignSchedule{
		SendStartHour: 9, SendEndHour: 18, SendDays: 31,
		DailyCapPerMailbox: 100, MinIntervalSec: 90, WarmupEnabled: true,
		BouncePauseThreshold: 5,
	})
}

// apply はベース (既定値 or 現在値) の上に、指定されたフィールドだけを
// 上書きして返す。update_campaign_draft では UpdateCampaign RPC の schedule
// が全量置換なため、現在値をベースに渡して部分更新のセマンティクスにする。
func (in *campaignScheduleIn) apply(p *campaignv1.CampaignSchedule) *campaignv1.CampaignSchedule {
	if in.SendStartHour != nil {
		p.SendStartHour = *in.SendStartHour
	}
	if in.SendEndHour != nil {
		p.SendEndHour = *in.SendEndHour
	}
	if in.SendDays != nil {
		p.SendDays = *in.SendDays
	}
	if in.DailyCapPerMailbox != nil {
		p.DailyCapPerMailbox = *in.DailyCapPerMailbox
	}
	if in.MinIntervalSec != nil {
		p.MinIntervalSec = *in.MinIntervalSec
	}
	if in.WarmupEnabled != nil {
		p.WarmupEnabled = *in.WarmupEnabled
	}
	if in.BouncePauseThreshold != nil {
		p.BouncePauseThreshold = *in.BouncePauseThreshold
	}
	return p
}

type campaignSenderIn struct {
	SenderOrg     string `json:"sender_org" jsonschema:"sender organisation or person name (特定電子メール法の法定表示; the campaign cannot start while empty)"`
	SenderAddress string `json:"sender_address" jsonschema:"sender postal address (法定表示)"`
	SenderContact string `json:"sender_contact" jsonschema:"sender phone number or contact point (法定表示)"`
}

type createCampaignDraftIn struct {
	Name        string               `json:"name" jsonschema:"campaign name (internal label shown in the UI)"`
	CustomerIDs []string             `json:"customer_ids,omitempty" jsonschema:"recipient customer UUIDs (immutable snapshot, max 10000; at least one of customer_ids/book_ids is required). Customers without an email address, suppressed (unsubscribed/bounced) or duplicated addresses are recorded as skipped — the breakdown is returned"`
	BookIDs     []string             `json:"book_ids,omitempty" jsonschema:"book UUIDs — every customer in these books is expanded into the recipient snapshot server-side; combinable with customer_ids (union, deduped). Requires editor role on each book. Use this instead of listing hundreds of customer_ids"`
	MailboxIDs  []string             `json:"mailbox_ids" jsonschema:"sending mailbox pool UUIDs (from list_mailboxes); requires editor role on every mailbox. Sends rotate across the pool"`
	Subject     string               `json:"subject" jsonschema:"first email subject. Placeholders: {{customer_name}} {{customer_corporation}} {{customer_mail}} {{customer_phone}} {{sender_name}} {{sender_mail}} {{today}}, plus {{fields.<key>}} for any per-customer merge variable (Customer.custom_fields, e.g. columns imported by import_customers_csv). Unknown {{fields.*}} render as an empty string"`
	Body        string               `json:"body" jsonschema:"first email plain-text body (same placeholders as subject, plus {{unsubscribe_url}}). Per-customer merge variables ({{fields.<key>}}) may contain newlines — they become line breaks in the HTML part. The 特定電子メール法 footer (sender info + unsubscribe link) is always appended automatically"`
	Followups   []campaignFollowupIn `json:"followups,omitempty" jsonschema:"followup emails (2nd mail and later, max 5) sent to recipients who have not replied; each waits wait_days after the previous step and threads as a reply"`
	Schedule    *campaignScheduleIn  `json:"schedule,omitempty" jsonschema:"pacing settings; omit for the defaults (JST 9-18, weekdays, 100/day/mailbox, 90s interval, warmup on)"`
	Sender      *campaignSenderIn    `json:"sender,omitempty" jsonschema:"特定電子メール法 sender disclosure printed in every mail footer. Can be omitted on the draft but must be set before start_campaign succeeds"`
	TrackOpens  bool                 `json:"track_opens,omitempty" jsonschema:"embed an open-tracking pixel"`
	TrackClicks bool                 `json:"track_clicks,omitempty" jsonschema:"rewrite links for click tracking"`
}

// updateCampaignDraftIn — campaign_id 以外は全フィールド省略可 (部分更新)。
// scalar は UpdateCampaign RPC の optional にそのまま乗る。schedule は RPC 側が
// 全量置換なので get→merge で部分更新に見せる。sender / mailbox_ids / followups
// は RPC のセマンティクス通り「指定時のみ全置換」。
type updateCampaignDraftIn struct {
	CampaignID  string               `json:"campaign_id" jsonschema:"campaign UUID (from list_campaigns or create_campaign_draft)"`
	Name        *string              `json:"name,omitempty" jsonschema:"new campaign name; omit to keep the current one"`
	Subject     *string              `json:"subject,omitempty" jsonschema:"new first-email subject (same placeholders as create_campaign_draft); omit to keep"`
	Body        *string              `json:"body,omitempty" jsonschema:"new first-email plain-text body; omit to keep"`
	TrackOpens  *bool                `json:"track_opens,omitempty" jsonschema:"enable/disable the open-tracking pixel; omit to keep"`
	TrackClicks *bool                `json:"track_clicks,omitempty" jsonschema:"enable/disable click-tracking link rewriting; omit to keep"`
	Followups   []campaignFollowupIn `json:"followups,omitempty" jsonschema:"REPLACES ALL followup steps when given (send the full list, max 5, in order); omit or empty = keep the current steps unchanged (there is no way to delete all followups with this tool)"`
	Schedule    *campaignScheduleIn  `json:"schedule,omitempty" jsonschema:"pacing changes; only the schedule fields you pass change, the rest keep their current values"`
	Sender      *campaignSenderIn    `json:"sender,omitempty" jsonschema:"特定電子メール法 sender disclosure; when given all 3 fields are replaced together, so pass all of them"`
	MailboxIDs  []string             `json:"mailbox_ids,omitempty" jsonschema:"REPLACES the whole sending mailbox pool when given (requires editor role on every new mailbox); omit or empty = keep the current pool"`
}

// ─── registration ───────────────────────────────────────────────

func addTools(s *mcp.Server, deps Deps) {
	if deps.Mailbox != nil {
		mcp.AddTool(s, &mcp.Tool{
			Name: "list_mailboxes",
			Description: "List the mailboxes (real email addresses Phox owns) the authenticated " +
				"user can send from or read. Returns each mailbox's id, address and your role " +
				"(viewer/editor/owner); editor or owner is required to send from it.",
		}, func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, any, error) {
			resp, err := deps.Mailbox.ListMailboxes(ctx, connect.NewRequest(&mailboxv1.ListMailboxesRequest{}))
			return protoResult(resp, err)
		})

		mcp.AddTool(s, &mcp.Tool{
			Name: "list_mailbox_messages",
			Description: "List ingested emails of a mailbox (both received and sent), newest first — " +
				"including mail from senders that are NOT yet customers (new inquiries). Returns metadata " +
				"only (from/to/subject/date, customer_id when the sender is a known customer); fetch the " +
				"body with get_mailbox_message. Requires viewer access to the mailbox.",
		}, func(ctx context.Context, _ *mcp.CallToolRequest, in listMailboxMessagesIn) (*mcp.CallToolResult, any, error) {
			req := &mailboxv1.ListMailboxMessagesRequest{MailboxId: in.MailboxID}
			if in.Folder != "" {
				req.Folder = proto.String(in.Folder)
			}
			if in.Limit > 0 {
				req.Limit = proto.Int32(in.Limit)
			}
			if in.Offset > 0 {
				req.Offset = proto.Int32(in.Offset)
			}
			resp, err := deps.Mailbox.ListMailboxMessages(ctx, connect.NewRequest(req))
			return protoResult(resp, err)
		})

		mcp.AddTool(s, &mcp.Tool{
			Name: "get_mailbox_message",
			Description: "Fetch one ingested email including its plain-text body and attachment " +
				"filenames. Use the 'id' from list_mailbox_messages. Requires viewer access to the mailbox.",
		}, func(ctx context.Context, _ *mcp.CallToolRequest, in getMailboxMessageIn) (*mcp.CallToolResult, any, error) {
			resp, err := deps.Mailbox.GetMailboxMessage(ctx, connect.NewRequest(&mailboxv1.GetMailboxMessageRequest{
				Id: in.MessageID,
			}))
			return protoResult(resp, err)
		})
	}

	mcp.AddTool(s, &mcp.Tool{
		Name: "create_customer",
		Description: "Create a customer in a book (e.g. from an inquiry email found via " +
			"list_mailbox_messages). Upsert-safe: if 'mail' is given and a customer with that email " +
			"already exists in the book, the existing customer is returned (and any 'contacts' are " +
			"still added to it). Optionally attach 'contacts' (extra email addresses of the same " +
			"customer) to aggregate their mailbox history. Optionally set 'custom_fields' — arbitrary " +
			"per-customer merge variables referenced in campaign bodies as {{fields.<key>}}. " +
			"Requires editor access to the book.",
		InputSchema: mcpInputSchema[createCustomerIn](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createCustomerIn) (*mcp.CallToolResult, any, error) {
		// upsert 判定: mail 一致の既存顧客がいれば作らずにそれを返す。
		// 生クエリの結果は必ず authz 付きの GetCustomer を通して返す。
		if in.Mail != "" && deps.Queries != nil {
			bookID, perr := uuid.Parse(in.BookID)
			if perr != nil {
				return nil, nil, fmt.Errorf("book_id: %w", perr)
			}
			if existing, ferr := deps.Queries.FindCustomerByBookAndEmail(ctx, db.FindCustomerByBookAndEmailParams{
				BookID: bookID,
				Mail:   strings.ToLower(strings.TrimSpace(in.Mail)),
			}); ferr == nil {
				// 既存でも、未紐付けの過去メールを履歴に取り込む (editor 必須・冪等)。
				if berr := deps.Customer.BackfillMailboxTimeline(ctx, bookID, existing, in.Mail); berr != nil {
					return nil, nil, berr
				}
				if cerr := syncCustomerContacts(ctx, deps, existing, in.Contacts); cerr != nil {
					return nil, nil, cerr
				}
				resp, gerr := deps.Customer.GetCustomer(ctx, connect.NewRequest(&customerv1.GetCustomerRequest{
					Id: existing.String(),
				}))
				return protoResult(resp, gerr)
			}
		}
		resp, err := deps.Customer.CreateCustomer(ctx, connect.NewRequest(&customerv1.CreateCustomerRequest{
			BookId:      in.BookID,
			Name:        in.Name,
			Mail:        in.Mail,
			Phone:       in.Phone,
			Corporation: in.Corporation,
			Category:    in.Category,
			Address:     in.Address,
			Memo:        in.Memo,

			CustomFields: in.CustomFields,
		}))
		if err != nil {
			return protoResult(resp, err)
		}
		if newID, perr := uuid.Parse(resp.Msg.Customer.Id); perr == nil {
			if cerr := syncCustomerContacts(ctx, deps, newID, in.Contacts); cerr != nil {
				return nil, nil, cerr
			}
		}
		return protoResult(resp, err)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "import_customers_csv",
		Description: "Bulk-import customers from CSV text. CREATES A NEW BOOK named book_name, owned " +
			"by the authenticated user, and inserts one customer per data row — it cannot append to an " +
			"existing book (use create_customer for that). The first CSV line must be a header row; " +
			"recognised columns (case-insensitive) are: name, corporation, phone, mail (or email), " +
			"category, address, memo, and optionally id (customer UUID, auto-generated when absent). " +
			"EVERY OTHER COLUMN is kept as a per-customer merge variable: a column named meo_score " +
			"becomes {{fields.meo_score}} in campaign subjects and bodies, so one CSV can drive a " +
			"mail whose content differs per recipient (e.g. a per-store MEO diagnosis). Column names " +
			"are lowercased with non [a-z0-9_] characters replaced by '_'; at most 32 such columns and " +
			"4096 bytes per cell are kept. Cell values are plain text and may contain newlines — they " +
			"render as line breaks in the HTML part of the mail. Use enrich_customer_rows to normalise " +
			"messy canonical columns. Missing/empty cells are " +
			"fine (stored as empty strings — rows without an email import too). Max 50,000 rows. " +
			"Returns book_id, imported/failed counts and per-line errors for rows that could not be " +
			"inserted (e.g. malformed CSV line or invalid id). No email is involved at any point.",
		InputSchema: mcpInputSchema[importCustomersCSVIn](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in importCustomersCSVIn) (*mcp.CallToolResult, any, error) {
		if strings.TrimSpace(in.BookName) == "" {
			return nil, nil, fmt.Errorf("book_name: required")
		}
		if strings.TrimSpace(in.CSVContent) == "" {
			return nil, nil, fmt.Errorf("csv_content: required")
		}
		// owner はツール引数ではなく認証済みトークンの subject。呼び出し元が
		// 他人を owner に指定できてはいけない (ImportBook RPC 自体は owner_id を
		// 信用するので、ここで固定するのが MCP 経路の権限境界)。
		token, err := auth.AuthorizeUser(ctx)
		if err != nil {
			return nil, nil, err
		}
		// ImportBook は file_name から ".csv" を落としたものを book 名にする。
		resp, rpcErr := deps.Book.ImportBook(ctx, connect.NewRequest(&bookv1.ImportBookRequest{
			FileName:    in.BookName + ".csv",
			FileContent: []byte(in.CSVContent),
			OwnerId:     token.Subject(),
		}))
		return protoResult(resp, rpcErr)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "enrich_customer_rows",
		Description: "Normalise/enrich messy customer-list rows with the server-side LLM (Gemini) — " +
			"suggestions only, NOTHING is written to the database. Give the raw CSV headers (any " +
			"labels, Japanese OK) and raw rows; returns, per row, proposed values for the canonical " +
			"fields (phone/category/name/corporation/address/memo/mail) with a confidence score and a " +
			"changed flag. To apply accepted suggestions, reshape them yourself into " +
			"import_customers_csv (new book) or create_customer calls. Max 500 rows per call — split " +
			"larger lists. Fails with failed_precondition when the server has no GEMINI_API_KEY " +
			"configured (currently the case in production) — calling it is also how you detect " +
			"whether AI enrichment is enabled in this environment.",
		InputSchema: mcpInputSchema[enrichCustomerRowsIn](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in enrichCustomerRowsIn) (*mcp.CallToolResult, any, error) {
		// proto の buf.validate 相当の前置チェック (in-process 呼び出しは
		// validate インターセプタを通らない)。headers/rows の必須・500 行上限は
		// サービス側でも検査されるが、64 列上限はここでしか守れない。
		if len(in.Headers) > 64 {
			return nil, nil, fmt.Errorf("headers: at most 64 columns (got %d)", len(in.Headers))
		}
		req := &customerv1.EnrichCustomerRowsRequest{
			Headers:      in.Headers,
			TargetFields: in.TargetFields,
		}
		for i, cells := range in.Rows {
			if len(cells) > 64 {
				return nil, nil, fmt.Errorf("rows[%d]: at most 64 cells (got %d)", i, len(cells))
			}
			req.Rows = append(req.Rows, &customerv1.CustomerRowValues{Cells: cells})
		}
		resp, err := deps.Customer.EnrichCustomerRows(ctx, connect.NewRequest(req))
		return protoResult(resp, err)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "list_books",
		Description: "List the customer books (顧客リスト) the authenticated user can access. " +
			"Returns book ids you can feed into the other tools.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, any, error) {
		resp, err := deps.Book.ListBooks(ctx, connect.NewRequest(&bookv1.ListBooksRequest{}))
		return protoResult(resp, err)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "search_customers",
		Description: "Full-text search across customers in every book the user has access to " +
			"(Elasticsearch, Japanese-aware). Supports prefecture filtering and pagination.",
		InputSchema: mcpInputSchema[searchCustomersIn](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in searchCustomersIn) (*mcp.CallToolResult, any, error) {
		resp, err := deps.Search.SearchCustomers(ctx, connect.NewRequest(&searchv1.SearchCustomersRequest{
			Query:      in.Query,
			Prefecture: in.Prefecture,
			BookIds:    in.BookIDs,
			Limit:      in.Limit,
			Offset:     in.Offset,
		}))
		return protoResult(resp, err)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_customer",
		Description: "Fetch one customer (profile, contacts, memo) by UUID. Requires viewer access to the customer's book.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getCustomerIn) (*mcp.CallToolResult, any, error) {
		resp, err := deps.Customer.GetCustomer(ctx, connect.NewRequest(&customerv1.GetCustomerRequest{
			Id: in.CustomerID,
		}))
		return protoResult(resp, err)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_customer_activities",
		Description: "Activity timeline (calls, sent/received emails) for a single customer, newest first.",
		InputSchema: mcpInputSchema[listCustomerActivitiesIn](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listCustomerActivitiesIn) (*mcp.CallToolResult, any, error) {
		types, err := activityTypes(in.Types)
		if err != nil {
			return nil, nil, err
		}
		resp, rpcErr := deps.Activity.ListActivitiesByCustomerID(ctx, connect.NewRequest(&activityv1.ListActivitiesByCustomerIDRequest{
			CustomerId: in.CustomerID,
			Types:      types,
		}))
		return protoResult(resp, rpcErr)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "list_book_activities",
		Description: "Book-wide activity feed: every customer's calls and emails in one timeline, " +
			"filterable by type, assignee and time range. Paginated (server default 50, max 200).",
		InputSchema: mcpInputSchema[listBookActivitiesIn](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listBookActivitiesIn) (*mcp.CallToolResult, any, error) {
		types, err := activityTypes(in.Types)
		if err != nil {
			return nil, nil, err
		}
		req := &activityv1.ListActivitiesByBookIDRequest{
			BookId: in.BookID,
			Types:  types,
			Limit:  in.Limit,
			Offset: in.Offset,
		}
		if in.UserID != "" {
			req.UserId = proto.String(in.UserID)
		}
		if req.OccurredFrom, err = parseRFC3339(in.OccurredFrom, "occurred_from"); err != nil {
			return nil, nil, err
		}
		if req.OccurredTo, err = parseRFC3339(in.OccurredTo, "occurred_to"); err != nil {
			return nil, nil, err
		}
		resp, rpcErr := deps.Activity.ListActivitiesByBookID(ctx, connect.NewRequest(req))
		return protoResult(resp, rpcErr)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "get_call_stats",
		Description: "Cross-tabulated call statistics for a book: one cell per (assignee, call outcome status) " +
			"with counts and total Zoom call duration.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in statsIn) (*mcp.CallToolResult, any, error) {
		req := &activityv1.GetCallStatsRequest{BookId: in.BookID}
		var err error
		if req.OccurredFrom, err = parseRFC3339(in.OccurredFrom, "occurred_from"); err != nil {
			return nil, nil, err
		}
		if req.OccurredTo, err = parseRFC3339(in.OccurredTo, "occurred_to"); err != nil {
			return nil, nil, err
		}
		resp, rpcErr := deps.Activity.GetCallStats(ctx, connect.NewRequest(req))
		return protoResult(resp, rpcErr)
	})

	// 唯一の書き込み tool (v1.1)。既存 RPC CreateActivityEmailSent に委譲する
	// ので、editor 権限チェック・SMTP 送信・Activity 記録・From 解決 (トークン
	// の email claim) はすべてサービス層の挙動そのまま。
	mcp.AddTool(s, &mcp.Tool{
		Name: "send_customer_email",
		Description: "Send an email to a customer through the configured SMTP relay and record it " +
			"as an email_sent activity on their timeline. The From address is the authenticated " +
			"user's email (Keycloak profile). Requires editor access to the customer's book. " +
			"NOTE: this actually sends mail — on staging SMTP is a MailHog sink (nothing is " +
			"delivered); in production it is real delivery.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in sendCustomerEmailIn) (*mcp.CallToolResult, any, error) {
		req := &activityv1.CreateActivityEmailSentRequest{
			CustomerId: in.CustomerID,
			MailTo:     in.MailTo,
			Subject:    in.Subject,
			Body:       in.Body,
		}
		if in.MailCc != "" {
			req.MailCc = proto.String(in.MailCc)
		}
		if in.ContactID != "" {
			req.ContactId = proto.String(in.ContactID)
		}
		if in.MailboxID != "" {
			req.MailboxId = proto.String(in.MailboxID)
		}
		resp, err := deps.Activity.CreateActivityEmailSent(ctx, connect.NewRequest(req))
		return protoResult(resp, err)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "get_mail_stats",
		Description: "Per-assignee email statistics for a book: sent count and (approximate) reply count. " +
			"Replies are attributed to the last assignee who mailed that customer.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in statsIn) (*mcp.CallToolResult, any, error) {
		req := &activityv1.GetMailStatsRequest{BookId: in.BookID}
		var err error
		if req.OccurredFrom, err = parseRFC3339(in.OccurredFrom, "occurred_from"); err != nil {
			return nil, nil, err
		}
		if req.OccurredTo, err = parseRFC3339(in.OccurredTo, "occurred_to"); err != nil {
			return nil, nil, err
		}
		resp, rpcErr := deps.Activity.GetMailStats(ctx, connect.NewRequest(req))
		return protoResult(resp, rpcErr)
	})

	if deps.Campaign != nil {
		addCampaignTools(s, deps)
	}
}

// ─── campaign tools (Phase 27i) ─────────────────────────────────
//
// エージェント運用のゴール: 雑な CSV/台本 → create_customer で正規化登録 →
// create_campaign_draft で下書き作成 → 人がレビュー → start_campaign。
// 送信を伴うのは start_campaign だけで、下書き作成はどれだけ呼んでも
// 実メールは 1 通も出ない。RBAC は CampaignService の in-process 呼び出しに
// そのまま乗る (プール全 Mailbox editor + 受信者 Book editor)。
func addCampaignTools(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "list_campaigns",
		Description: "List the company's cold-email campaigns (newest first) with per-campaign " +
			"summary stats (queued/sent/opened/clicked/replied/bounced/unsubscribed). Returns the " +
			"campaign ids the other campaign tools take.",
		InputSchema: mcpInputSchema[listCampaignsIn](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listCampaignsIn) (*mcp.CallToolResult, any, error) {
		req := &campaignv1.ListCampaignsRequest{}
		if in.Limit > 0 {
			req.Limit = proto.Int32(in.Limit)
		}
		if in.Offset > 0 {
			req.Offset = proto.Int32(in.Offset)
		}
		resp, err := deps.Campaign.ListCampaigns(ctx, connect.NewRequest(req))
		return protoResult(resp, err)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "get_campaign",
		Description: "Fetch one campaign in full: status, subject/body template, followup steps, " +
			"schedule (pacing), 特定電子メール法 sender disclosure, mailbox pool, cumulative stats " +
			"and — while running — the estimated completion time.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in campaignIDIn) (*mcp.CallToolResult, any, error) {
		resp, err := deps.Campaign.GetCampaign(ctx, connect.NewRequest(&campaignv1.GetCampaignRequest{
			Id: in.CampaignID,
		}))
		return protoResult(resp, err)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "create_campaign_draft",
		Description: "Create a cold-email campaign as a DRAFT — no email is sent by this tool, ever. " +
			"The recipient list is snapshotted from customer_ids and/or book_ids (skipping customers " +
			"without an email, suppressed addresses, duplicates and no-MX domains; the breakdown is " +
			"returned). Prefer book_ids to target whole books — the server expands them, so you never " +
			"need to enumerate hundreds of customer_ids. " +
			"Requires editor role on every mailbox in mailbox_ids and on every book the recipients " +
			"belong to. Present the draft (subject, body, recipients, schedule, sender disclosure) to " +
			"the user for review; actually sending requires an explicit start_campaign call.",
		InputSchema: mcpInputSchema[createCampaignDraftIn](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createCampaignDraftIn) (*mcp.CallToolResult, any, error) {
		// in-process 呼び出しは Connect の buf.validate インターセプタを通らない
		// ので、proto 制約のうち DB まで行ってから壊れると分かりにくいものだけ
		// followupsToProto で先に検査する。
		followups, ferr := followupsToProto(in.Followups)
		if ferr != nil {
			return nil, nil, ferr
		}
		req := &campaignv1.CreateCampaignRequest{
			Name:        in.Name,
			CustomerIds: in.CustomerIDs,
			BookIds:     in.BookIDs,
			MailboxIds:  in.MailboxIDs,
			Subject:     in.Subject,
			Body:        in.Body,
			TrackOpens:  in.TrackOpens,
			TrackClicks: in.TrackClicks,
			Followups:   followups,
		}
		if in.Schedule != nil {
			req.Schedule = in.Schedule.toProto()
		}
		if in.Sender != nil {
			req.Sender = &campaignv1.CampaignSender{
				SenderOrg:     in.Sender.SenderOrg,
				SenderAddress: in.Sender.SenderAddress,
				SenderContact: in.Sender.SenderContact,
			}
		}
		resp, err := deps.Campaign.CreateCampaign(ctx, connect.NewRequest(req))
		res, out, rerr := protoResult(resp, err)
		if rerr != nil {
			return nil, nil, rerr
		}
		// 下書きのままであること・開始は別操作であることをモデルに明示する。
		res.Content = append(res.Content, &mcp.TextContent{
			Text: "注意: このキャンペーンは draft のまま作成されました。メールはまだ 1 通も送信されていません。" +
				"queued/skipped の内訳と本文をユーザーに提示し、明示的な承認を得てから start_campaign を呼んでください " +
				"(start_campaign は実メールの送信を開始します)。",
		})
		return res, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "update_campaign_draft",
		Description: "Update a campaign after create_campaign_draft — partial update: only the fields " +
			"you pass change, everything omitted keeps its current value. Editable: name, subject, body, " +
			"followups (passing them REPLACES all steps — send the full list), schedule (per-field " +
			"partial), 特定電子メール法 sender disclosure (all 3 fields replaced together), mailbox pool " +
			"(REPLACES the whole pool) and open/click tracking. Only campaigns in status draft or paused " +
			"can be edited; running/completed/cancelled ones fail with failed_precondition (pause a " +
			"running campaign first). The recipient snapshot cannot be changed — cancel and recreate " +
			"instead. Like create_campaign_draft, this never sends any email and never starts the " +
			"campaign; starting still requires an explicit start_campaign call.",
		InputSchema: mcpInputSchema[updateCampaignDraftIn](),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in updateCampaignDraftIn) (*mcp.CallToolResult, any, error) {
		// create_campaign_draft と同じ理由 (in-process 呼び出しは buf.validate を
		// 通らない) で followups をここで検査する。
		followups, ferr := followupsToProto(in.Followups)
		if ferr != nil {
			return nil, nil, ferr
		}
		req := &campaignv1.UpdateCampaignRequest{
			Id:          in.CampaignID,
			Name:        in.Name,
			Subject:     in.Subject,
			Body:        in.Body,
			TrackOpens:  in.TrackOpens,
			TrackClicks: in.TrackClicks,
			MailboxIds:  in.MailboxIDs,
			Followups:   followups,
		}
		if in.Schedule != nil {
			// UpdateCampaign RPC の schedule は全量置換なので、現在値を読んで
			// 未指定フィールドを埋める (get→merge)。GetCampaign は同じ RBAC を
			// 通るため、ここで読めなければどのみち更新もできない。
			cur, gerr := deps.Campaign.GetCampaign(ctx, connect.NewRequest(&campaignv1.GetCampaignRequest{
				Id: in.CampaignID,
			}))
			if gerr != nil {
				return protoResult(cur, gerr)
			}
			base := cur.Msg.Campaign.GetSchedule()
			if base == nil {
				req.Schedule = in.Schedule.toProto() // 現在値なし → サービス既定値ベース
			} else {
				req.Schedule = in.Schedule.apply(proto.Clone(base).(*campaignv1.CampaignSchedule))
			}
		}
		if in.Sender != nil {
			req.Sender = &campaignv1.CampaignSender{
				SenderOrg:     in.Sender.SenderOrg,
				SenderAddress: in.Sender.SenderAddress,
				SenderContact: in.Sender.SenderContact,
			}
		}
		resp, err := deps.Campaign.UpdateCampaign(ctx, connect.NewRequest(req))
		return protoResult(resp, err)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "start_campaign",
		Description: "実メールを送信する操作。ユーザーの明示的な確認を得てから呼ぶこと。" +
			"Start (or resume from paused) a campaign: status becomes running and the send worker " +
			"begins emailing the queued recipients within the schedule window. Fails with " +
			"failed_precondition when the 特定電子メール法 sender disclosure " +
			"(sender_org/sender_address/sender_contact) or subject/body is still empty.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in campaignIDIn) (*mcp.CallToolResult, any, error) {
		resp, err := deps.Campaign.StartCampaign(ctx, connect.NewRequest(&campaignv1.StartCampaignRequest{
			Id: in.CampaignID,
		}))
		return protoResult(resp, err)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "pause_campaign",
		Description: "Pause a running campaign (running → paused). Sending stops after the " +
			"in-flight mail; resume later with start_campaign.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in campaignIDIn) (*mcp.CallToolResult, any, error) {
		resp, err := deps.Campaign.PauseCampaign(ctx, connect.NewRequest(&campaignv1.PauseCampaignRequest{
			Id: in.CampaignID,
		}))
		return protoResult(resp, err)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "cancel_campaign",
		Description: "Cancel a campaign permanently (draft/running/paused → cancelled; cannot be " +
			"restarted). Use this to discard a draft the user rejected.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in campaignIDIn) (*mcp.CallToolResult, any, error) {
		resp, err := deps.Campaign.CancelCampaign(ctx, connect.NewRequest(&campaignv1.CancelCampaignRequest{
			Id: in.CampaignID,
		}))
		return protoResult(resp, err)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "get_campaign_stats",
		Description: "Dashboard numbers for one campaign: cumulative stats (total/queued/sent/failed/" +
			"opened/clicked/replied/bounced/unsubscribed/waiting_followup) plus the daily timeseries " +
			"(sent/opened/clicked/replied/bounced/unsubscribed per JST day).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in campaignIDIn) (*mcp.CallToolResult, any, error) {
		getResp, err := deps.Campaign.GetCampaign(ctx, connect.NewRequest(&campaignv1.GetCampaignRequest{
			Id: in.CampaignID,
		}))
		if err != nil {
			return protoResult(getResp, err)
		}
		tsResp, err := deps.Campaign.GetCampaignTimeseries(ctx, connect.NewRequest(&campaignv1.GetCampaignTimeseriesRequest{
			CampaignId: in.CampaignID,
		}))
		if err != nil {
			return protoResult(tsResp, err)
		}
		c := getResp.Msg.Campaign
		statsJSON, err := protojson.MarshalOptions{EmitUnpopulated: true}.Marshal(c.Stats)
		if err != nil {
			return nil, nil, fmt.Errorf("marshal stats: %w", err)
		}
		daysJSON, err := protojson.MarshalOptions{EmitUnpopulated: true}.Marshal(tsResp.Msg)
		if err != nil {
			return nil, nil, fmt.Errorf("marshal timeseries: %w", err)
		}
		combined, err := json.Marshal(map[string]any{
			"campaignId": c.Id,
			"name":       c.Name,
			"status":     c.Status,
			"stats":      json.RawMessage(statsJSON),
			"timeseries": json.RawMessage(daysJSON),
		})
		if err != nil {
			return nil, nil, fmt.Errorf("marshal combined stats: %w", err)
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(combined)}},
		}, nil, nil
	})
}

// followupsToProto は create_campaign_draft / update_campaign_draft 共通の
// フォローアップ検証と proto 変換。in-process 呼び出しは Connect の
// buf.validate インターセプタを通らないので、DB まで行ってから壊れると
// 分かりにくい制約 (max 5 / wait_days 1-60 / body 必須) をここで検査する。
func followupsToProto(ins []campaignFollowupIn) ([]*campaignv1.CampaignFollowup, error) {
	if len(ins) > 5 {
		return nil, fmt.Errorf("followups: at most 5 steps (got %d)", len(ins))
	}
	out := make([]*campaignv1.CampaignFollowup, 0, len(ins))
	for i, fu := range ins {
		if fu.WaitDays < 1 || fu.WaitDays > 60 {
			return nil, fmt.Errorf("followups[%d].wait_days: must be 1-60 (got %d)", i, fu.WaitDays)
		}
		if fu.Body == "" {
			return nil, fmt.Errorf("followups[%d].body: required", i)
		}
		out = append(out, &campaignv1.CampaignFollowup{
			WaitDays: fu.WaitDays,
			Subject:  fu.Subject,
			Body:     fu.Body,
		})
	}
	return out, nil
}

// syncCustomercontacts は create_customer の contacts を顧客配下に登録する。
// 同一 mail の contact が既にあれば作らず (冪等)、各 contact の mail に一致する
// 未紐付けメールを顧客タイムラインに contact_id 付きでバックフィルする。
func syncCustomerContacts(ctx context.Context, deps Deps, customerID uuid.UUID, contacts []createCustomerContactIn) error {
	if len(contacts) == 0 || deps.Contact == nil || deps.Queries == nil {
		return nil
	}
	// 既存 contact を mail で index (冪等判定用)。
	existing, err := deps.Queries.ListContacts(ctx, customerID)
	if err != nil {
		return err
	}
	byMail := map[string]uuid.UUID{}
	for _, c := range existing {
		if c.Mail != "" {
			byMail[strings.ToLower(strings.TrimSpace(c.Mail))] = c.ID
		}
	}

	for _, in := range contacts {
		mail := strings.ToLower(strings.TrimSpace(in.Mail))
		if mail == "" {
			continue
		}
		contactID, ok := byMail[mail]
		if !ok {
			req := &contactv1.CreateContactRequest{
				CustomerId: customerID.String(),
				Name:       in.Name,
				Mail:       proto.String(in.Mail),
			}
			if in.Phone != "" {
				req.Phone = proto.String(in.Phone)
			}
			resp, cerr := deps.Contact.CreateContact(ctx, connect.NewRequest(req))
			if cerr != nil {
				return cerr
			}
			cid, perr := uuid.Parse(resp.Msg.CreatedContact.Id)
			if perr != nil {
				return perr
			}
			contactID = cid
			byMail[mail] = contactID
		}
		// この contact の mail に一致する過去メールを顧客タイムラインへ (contact_id 付き)。
		customer.BackfillContactMail(ctx, deps.Queries, customerID, contactID, in.Mail)
	}
	return nil
}

// ─── helpers ────────────────────────────────────────────────────

// mcpInputSchema builds the JSON Schema for a tool input type and normalizes
// nullable unions. jsonschema-go (v0.4) emits `["null","array"]` for every
// slice and `["null","string"]` for pointers; the union type makes some MCP
// clients serialise the value as a JSON *string* (observed with array params
// like contacts / book_ids: `has type "string", want one of "null, array"`).
// Collapsing to a plain type keeps clients sending real arrays/values.
func mcpInputSchema[In any]() *jsonschema.Schema {
	s, err := jsonschema.For[In](nil)
	if err != nil {
		panic(fmt.Sprintf("mcpserver: input schema for %T: %v", *new(In), err))
	}
	normalizeNullableUnions(s)
	return s
}

func normalizeNullableUnions(s *jsonschema.Schema) {
	if s == nil {
		return
	}
	if len(s.Types) > 0 {
		kept := make([]string, 0, len(s.Types))
		for _, t := range s.Types {
			if t != "null" {
				kept = append(kept, t)
			}
		}
		if len(kept) == 1 {
			s.Type = kept[0]
			s.Types = nil
		} else {
			s.Types = kept
		}
	}
	normalizeNullableUnions(s.Items)
	normalizeNullableUnions(s.AdditionalProperties)
	for _, p := range s.Properties {
		normalizeNullableUnions(p)
	}
	for _, p := range s.PrefixItems {
		normalizeNullableUnions(p)
	}
}

// protoResult converts a Connect service response into an MCP tool result:
// protojson for the payload (same shape the Connect API returns to the UI),
// and Connect error messages surfaced as tool errors (the SDK sets IsError
// when a ToolHandlerFor returns a non-nil error).
func protoResult[T any](resp *connect.Response[T], err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		var cerr *connect.Error
		if errors.As(err, &cerr) {
			return nil, nil, fmt.Errorf("%s: %s", cerr.Code(), cerr.Message())
		}
		return nil, nil, err
	}
	msg, ok := any(resp.Msg).(proto.Message)
	if !ok {
		return nil, nil, fmt.Errorf("internal: response %T is not a proto.Message", resp.Msg)
	}
	b, err := protojson.MarshalOptions{EmitUnpopulated: true}.Marshal(msg)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal response: %w", err)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(b)}},
	}, nil, nil
}

var activityTypeByName = map[string]activityv1.ActivityType{
	"call":           activityv1.ActivityType_ACTIVITY_TYPE_CALL,
	"email_sent":     activityv1.ActivityType_ACTIVITY_TYPE_EMAIL_SENT,
	"email_received": activityv1.ActivityType_ACTIVITY_TYPE_EMAIL_RECEIVED,
}

func activityTypes(names []string) ([]activityv1.ActivityType, error) {
	out := make([]activityv1.ActivityType, 0, len(names))
	for _, n := range names {
		t, ok := activityTypeByName[n]
		if !ok {
			return nil, fmt.Errorf("unknown activity type %q (want call | email_sent | email_received)", n)
		}
		out = append(out, t)
	}
	return out, nil
}

// parseRFC3339 converts an optional RFC3339 string into a protobuf Timestamp.
// Empty input means "unbounded" and returns nil.
func parseRFC3339(s, field string) (*timestamppb.Timestamp, error) {
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil, fmt.Errorf("%s: invalid RFC3339 timestamp %q: %v", field, s, err)
	}
	return timestamppb.New(t), nil
}
