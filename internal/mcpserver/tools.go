package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	activityv1 "github.com/0utl1er-tech/phox-customer/gen/pb/activity/v1"
	bookv1 "github.com/0utl1er-tech/phox-customer/gen/pb/book/v1"
	customerv1 "github.com/0utl1er-tech/phox-customer/gen/pb/customer/v1"
	mailboxv1 "github.com/0utl1er-tech/phox-customer/gen/pb/mailbox/v1"
	searchv1 "github.com/0utl1er-tech/phox-customer/gen/pb/search/v1"
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

type sendCustomerEmailIn struct {
	CustomerID string `json:"customer_id" jsonschema:"customer UUID — the email is recorded as an activity on this customer's timeline"`
	MailTo     string `json:"mail_to" jsonschema:"recipient email address"`
	MailCc     string `json:"mail_cc,omitempty" jsonschema:"optional CC address"`
	Subject    string `json:"subject" jsonschema:"mail subject (required, min 1 char)"`
	Body       string `json:"body,omitempty" jsonschema:"plain-text mail body"`
	ContactID  string `json:"contact_id,omitempty" jsonschema:"optional contact UUID to associate the mail with"`
	MailboxID  string `json:"mailbox_id,omitempty" jsonschema:"optional mailbox UUID (from list_mailboxes) to send as — the From address becomes that mailbox and replies flow back to it; requires editor role on the mailbox. Omit for the legacy send-as-yourself behaviour"`
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
	}

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
}

// ─── helpers ────────────────────────────────────────────────────

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
