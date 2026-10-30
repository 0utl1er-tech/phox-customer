package mail_test

import (
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"

	phoxmail "github.com/0utl1er-tech/phox-customer/internal/mail"
)

// これらのテストは `internal/mail/imap.go` (IMAP client wrapper) の pure
// 機能を in-memory IMAP サーバー (imapmemserver) に対して検証する。
// backend の pgx/sqlc には一切触らないので、CI / ローカル env を汚さない。
//
// 本格的な worker → DB 統合テストは別途 build tag 付きで書く (将来)。

const (
	testUsername = "test-user"
	testPassword = "test-password"
)

type fakeIMAPServer struct {
	addr    string
	user    *imapmemserver.User
	closer  io.Closer
	listner net.Listener
}

func (f *fakeIMAPServer) stop() {
	_ = f.listner.Close()
	if f.closer != nil {
		_ = f.closer.Close()
	}
}

// newFakeServer は imapmemserver を listen させた fake server を返す。
// テストは `localhost:{random port}` に平文 (none TLS) で接続する。
func newFakeServer(t *testing.T) *fakeIMAPServer {
	t.Helper()

	memServer := imapmemserver.New()
	user := imapmemserver.NewUser(testUsername, testPassword)
	if err := user.Create("INBOX", nil); err != nil {
		t.Fatalf("create INBOX: %v", err)
	}
	if err := user.Create("Sent", nil); err != nil {
		t.Fatalf("create Sent: %v", err)
	}
	memServer.AddUser(user)

	srv := imapserver.New(&imapserver.Options{
		NewSession: func(_ *imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return memServer.NewSession(), nil, nil
		},
		InsecureAuth: true, // plain TCP → LOGIN 許可
		Caps: imap.CapSet{
			imap.CapIMAP4rev1: {},
			imap.CapIMAP4rev2: {},
		},
	})

	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		_ = srv.Serve(ln)
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		_ = srv.Close()
	})

	return &fakeIMAPServer{
		addr:    ln.Addr().String(),
		user:    user,
		closer:  srv,
		listner: ln,
	}
}

// appendSimpleMessage は user の指定 mailbox に RFC822 メッセージを append する。
func appendSimpleMessage(t *testing.T, f *fakeIMAPServer, mailbox, raw string) {
	t.Helper()
	lit := strings.NewReader(raw)
	if _, err := f.user.Append(mailbox, &literalReader{r: lit, size: int64(len(raw))}, &imap.AppendOptions{
		Time: time.Now(),
	}); err != nil {
		t.Fatalf("append to %q: %v", mailbox, err)
	}
}

// literalReader は imap.LiteralReader interface を満たす小さな wrapper。
type literalReader struct {
	r    *strings.Reader
	size int64
}

func (l *literalReader) Read(p []byte) (int, error) { return l.r.Read(p) }
func (l *literalReader) Size() int64                { return l.size }

// -----------------------------------------------------------------------------

func TestDialIMAP_LoginFetchSince_Sent(t *testing.T) {
	f := newFakeServer(t)

	// Sent に 2 件 append
	appendSimpleMessage(t, f, "Sent", simpleRawRFC822(
		"<msg-sent-001@phox.test>",
		"From: alice@phox.test\r\n"+
			"To: tanaka@example.com\r\n"+
			"Subject: Sent test 1\r\n",
	))
	appendSimpleMessage(t, f, "Sent", simpleRawRFC822(
		"<msg-sent-002@phox.test>",
		"From: alice@phox.test\r\n"+
			"To: yamada@example.com\r\n"+
			"Subject: Sent test 2\r\n",
	))

	host, port := splitHostPort(t, f.addr)
	client, err := phoxmail.DialIMAP(phoxmail.IMAPConnectConfig{
		Host:     host,
		Port:     port,
		Username: testUsername,
		Password: testPassword,
		TLSMode:  phoxmail.IMAPTLSNone,
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	msgs, err := client.FetchSince("Sent", time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("FetchSince: %v", err)
	}

	if len(msgs) != 2 {
		t.Fatalf("got %d msgs, want 2", len(msgs))
	}

	// Message-ID が正規化されている (<>が剥がれている) こと
	ids := map[string]bool{}
	for _, m := range msgs {
		ids[m.MessageID] = true
	}
	if !ids["msg-sent-001@phox.test"] {
		t.Errorf("msg-sent-001 not found. got ids = %v", ids)
	}
	if !ids["msg-sent-002@phox.test"] {
		t.Errorf("msg-sent-002 not found. got ids = %v", ids)
	}

	// 1 件目の envelope パースを確認
	var first phoxmail.ParsedMessage
	for _, m := range msgs {
		if m.MessageID == "msg-sent-001@phox.test" {
			first = m
			break
		}
	}
	if first.Subject != "Sent test 1" {
		t.Errorf("subject = %q, want %q", first.Subject, "Sent test 1")
	}
	if first.From != "alice@phox.test" {
		t.Errorf("from = %q, want alice@phox.test", first.From)
	}
	if len(first.To) != 1 || first.To[0] != "tanaka@example.com" {
		t.Errorf("to = %v, want [tanaka@example.com]", first.To)
	}
}

func TestDialIMAP_FetchSince_EmptyMailbox(t *testing.T) {
	f := newFakeServer(t)

	host, port := splitHostPort(t, f.addr)
	client, err := phoxmail.DialIMAP(phoxmail.IMAPConnectConfig{
		Host:     host,
		Port:     port,
		Username: testUsername,
		Password: testPassword,
		TLSMode:  phoxmail.IMAPTLSNone,
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	msgs, err := client.FetchSince("INBOX", time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("FetchSince: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages, got %d", len(msgs))
	}
}

func TestDialIMAP_LoginFailure(t *testing.T) {
	f := newFakeServer(t)

	host, port := splitHostPort(t, f.addr)
	_, err := phoxmail.DialIMAP(phoxmail.IMAPConnectConfig{
		Host:     host,
		Port:     port,
		Username: testUsername,
		Password: "wrong",
		TLSMode:  phoxmail.IMAPTLSNone,
	})
	if err == nil {
		t.Fatal("expected login error for wrong password")
	}
}

// -----------------------------------------------------------------------------
// helpers

func simpleRawRFC822(messageID, headers string) string {
	// 最小限の RFC822。Message-ID は caller が <...> 付きで渡す。
	return "MIME-Version: 1.0\r\n" +
		"Message-ID: " + messageID + "\r\n" +
		"Date: Fri, 11 Apr 2026 10:00:00 +0900\r\n" +
		headers +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"body\r\n"
}

func splitHostPort(t *testing.T, addr string) (string, int) {
	t.Helper()
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	var port int
	if _, err := parseInt(p, &port); err != nil {
		t.Fatalf("parse port: %v", err)
	}
	return h, port
}

// parseInt — Sscanf なし (test bin を小さく保つ)
func parseInt(s string, out *int) (int, error) {
	*out = 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, &strconvErr{s}
		}
		*out = *out*10 + int(r-'0')
	}
	return *out, nil
}

type strconvErr struct{ s string }

func (e *strconvErr) Error() string { return "not an int: " + e.s }

// sanity: both imports used
var _ = imapclient.New

// appendMessageAt は内部日付 (INTERNALDATE) を指定して append する。
func appendMessageAt(t *testing.T, f *fakeIMAPServer, mailbox, raw string, at time.Time) {
	t.Helper()
	lit := strings.NewReader(raw)
	if _, err := f.user.Append(mailbox, &literalReader{r: lit, size: int64(len(raw))}, &imap.AppendOptions{
		Time: at,
	}); err != nil {
		t.Fatalf("append to %q: %v", mailbox, err)
	}
}

func rawMail(id, subject string) string {
	return "From: a@example.com\r\nTo: b@example.com\r\nMessage-ID: <" + id + "@example.com>\r\n" +
		"Subject: " + subject + "\r\nDate: Mon, 01 Jan 2024 00:00:00 +0000\r\n\r\nbody\r\n"
}

// SEARCH SINCE を使わずに since 以降だけを取れること (Apache James 対策)。
// 1 回目: INTERNALDATE で絞る。2 回目以降: カーソル (UIDNEXT) の続きだけ取る。
func TestFetchSince_CursorIncremental(t *testing.T) {
	f := newFakeServer(t)
	now := time.Now()
	appendMessageAt(t, f, "INBOX", rawMail("old", "old"), now.Add(-72*time.Hour))
	appendMessageAt(t, f, "INBOX", rawMail("recent", "recent"), now.Add(-1*time.Hour))

	cursor := phoxmail.NewUIDCursor()
	dial := func() *phoxmail.IMAPClient {
		t.Helper()
		host, port := splitHostPort(t, f.addr)
		c, err := phoxmail.DialIMAP(phoxmail.IMAPConnectConfig{
			Host: host, Port: port, Username: testUsername, Password: testPassword, TLSMode: phoxmail.IMAPTLSNone,
		})
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		return c.WithCursor(cursor)
	}

	// 1 回目 (カーソル無し): 24h 以内の 1 通だけ
	c := dial()
	msgs, err := c.FetchSince("INBOX", now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("fetch 1: %v", err)
	}
	if len(msgs) != 1 || msgs[0].MessageID != "recent@example.com" {
		t.Fatalf("fetch 1: want only recent, got %+v", msgs)
	}
	_ = c.Close()

	// 2 回目 (新着なし): 0 通。古いメールを再び返さない
	c = dial()
	msgs, err = c.FetchSince("INBOX", now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("fetch 2: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("fetch 2: want 0, got %+v", msgs)
	}
	_ = c.Close()

	// 3 回目 (新着 1 通): since より前の内部日付でも UID が新しければ取る
	appendMessageAt(t, f, "INBOX", rawMail("new", "new"), now.Add(-100*time.Hour))
	c = dial()
	msgs, err = c.FetchSince("INBOX", now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("fetch 3: %v", err)
	}
	if len(msgs) != 1 || msgs[0].MessageID != "new@example.com" {
		t.Fatalf("fetch 3: want only new, got %+v", msgs)
	}
	_ = c.Close()

	// since ゼロ値 = 全件 (バックフィル)
	c = dial()
	msgs, err = c.FetchSince("INBOX", time.Time{})
	if err != nil {
		t.Fatalf("fetch all: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("fetch all: want 3, got %d", len(msgs))
	}
	_ = c.Close()
}
