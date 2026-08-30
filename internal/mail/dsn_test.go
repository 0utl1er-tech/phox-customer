package mail

import "testing"

const sampleDSN = "From: MAILER-DAEMON@mail.0utl1er.tech\r\n" +
	"To: sales@0utl1er.tech\r\n" +
	"Subject: Undelivered Mail Returned to Sender\r\n" +
	"Content-Type: multipart/report; report-type=delivery-status; boundary=\"BOUND\"\r\n" +
	"\r\n" +
	"--BOUND\r\n" +
	"Content-Type: text/plain\r\n" +
	"\r\n" +
	"This is the mail system at host mail.0utl1er.tech.\r\n" +
	"--BOUND\r\n" +
	"Content-Type: message/delivery-status\r\n" +
	"\r\n" +
	"Reporting-MTA: dns; mail.0utl1er.tech\r\n" +
	"\r\n" +
	"Final-Recipient: rfc822; nobody@example.com\r\n" +
	"Action: failed\r\n" +
	"Status: 5.1.1\r\n" +
	"Diagnostic-Code: smtp; 550 5.1.1 User unknown\r\n" +
	"--BOUND\r\n" +
	"Content-Type: message/rfc822\r\n" +
	"\r\n" +
	"Message-ID: <cmp-11111111-2222-3333-4444-555555555555@0utl1er.tech>\r\n" +
	"Subject: original\r\n" +
	"\r\n" +
	"body\r\n" +
	"--BOUND--\r\n"

func TestParseDSN(t *testing.T) {
	info := ParseDSN([]byte(sampleDSN))
	if info == nil {
		t.Fatal("expected DSN, got nil")
	}
	if info.Action != "failed" || info.Status != "5.1.1" {
		t.Errorf("action/status = %q/%q", info.Action, info.Status)
	}
	if info.FinalRecipient != "nobody@example.com" {
		t.Errorf("final recipient = %q", info.FinalRecipient)
	}
	if info.OriginalMessageID != "cmp-11111111-2222-3333-4444-555555555555@0utl1er.tech" {
		t.Errorf("original message id = %q", info.OriginalMessageID)
	}
	if !info.IsHard() {
		t.Error("5.1.1 should be hard bounce")
	}
}

func TestParseDSNNonDSN(t *testing.T) {
	plain := "From: someone@example.com\r\nSubject: hi\r\nContent-Type: text/plain\r\n\r\nhello\r\n"
	if got := ParseDSN([]byte(plain)); got != nil {
		t.Fatalf("plain mail should not be DSN: %+v", got)
	}
	// multipart/alternative も DSN ではない
	alt := "Content-Type: multipart/alternative; boundary=\"X\"\r\n\r\n--X\r\nContent-Type: text/plain\r\n\r\nhi\r\n--X--\r\n"
	if got := ParseDSN([]byte(alt)); got != nil {
		t.Fatalf("multipart/alternative should not be DSN: %+v", got)
	}
}

func TestParseReferences(t *testing.T) {
	raw := "From: a@example.com\r\n" +
		"References: <cmp-aaa@0utl1er.tech>\r\n <other-id@example.com>\r\n" +
		"Content-Type: text/plain\r\n\r\nbody\r\n"
	refs := parseReferences([]byte(raw))
	if len(refs) != 2 || refs[0] != "cmp-aaa@0utl1er.tech" || refs[1] != "other-id@example.com" {
		t.Fatalf("parseReferences = %v", refs)
	}
}
