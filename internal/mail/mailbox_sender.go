package mail

import (
	"context"
	"errors"
	"fmt"

	gomail "github.com/wneessen/go-mail"
)

// MailboxSender は「Phox が所有する実メールボックス」として送信する。
// 共有 mailu ホスト (MAILU_SMTP_*) に対し、メールボックス毎の
// username/password で認証し、From もそのメールボックスにする。
//
// レガシーの SMTPClient (サービスアカウント + From なりすまし + Reply-To
// 固定) と違い、Reply-To は付けない — 返信は同じメールボックスに届き、
// IMAP 取込み (Phase C) がそのまま拾える。
type MailboxSender struct {
	host    string
	port    int
	tlsMode string
}

// NewMailboxSender returns nil when host is empty (feature disabled).
func NewMailboxSender(host string, port int, tlsMode string) (*MailboxSender, error) {
	if host == "" {
		return nil, nil
	}
	if port == 0 {
		return nil, errors.New("mail: MAILU_SMTP_PORT is required when MAILU_SMTP_HOST is set")
	}
	// TLS mode は接続時に検証されるが、typo は起動時に落とす。
	switch tlsMode {
	case "", "none", "implicit", "starttls":
	default:
		return nil, fmt.Errorf("mail: unknown MAILU_SMTP_TLS_MODE=%q (expected none|implicit|starttls)", tlsMode)
	}
	return &MailboxSender{host: host, port: port, tlsMode: tlsMode}, nil
}

// SendAs sends one message authenticated as the given mailbox credentials.
// クライアントは送信毎に生成する (CRM の送信量では接続プールは不要)。
func (m *MailboxSender) SendAs(
	ctx context.Context,
	smtpUsername, password string,
	fromAddr, fromName, to string,
	cc []string,
	subject, body, messageID string,
) error {
	if m == nil {
		return ErrNotConfigured
	}

	opts := []gomail.Option{gomail.WithPort(m.port)}
	switch m.tlsMode {
	case "", "none":
		opts = append(opts, gomail.WithTLSPolicy(gomail.NoTLS))
	case "implicit":
		opts = append(opts, gomail.WithSSL(), gomail.WithTLSPolicy(gomail.TLSMandatory))
	case "starttls":
		opts = append(opts, gomail.WithTLSPolicy(gomail.TLSMandatory))
	}
	if smtpUsername != "" {
		opts = append(opts,
			gomail.WithSMTPAuth(gomail.SMTPAuthPlain),
			gomail.WithUsername(smtpUsername),
			gomail.WithPassword(password),
		)
	}
	client, err := gomail.NewClient(m.host, opts...)
	if err != nil {
		return fmt.Errorf("mail: new mailbox client: %w", err)
	}

	msg := gomail.NewMsg()
	if fromName != "" {
		if err := msg.FromFormat(fromName, fromAddr); err != nil {
			return fmt.Errorf("mail: set from format: %w", err)
		}
	} else {
		if err := msg.From(fromAddr); err != nil {
			return fmt.Errorf("mail: set from: %w", err)
		}
	}
	if err := msg.To(to); err != nil {
		return fmt.Errorf("mail: set to: %w", err)
	}
	for _, c := range cc {
		if c == "" {
			continue
		}
		if err := msg.Cc(c); err != nil {
			return fmt.Errorf("mail: set cc: %w", err)
		}
	}
	// Reply-To は意図的に付けない: From = 実メールボックスなので返信は
	// そのまま同じ口に戻る。
	msg.Subject(subject)
	msg.SetBodyString(gomail.TypeTextPlain, body)
	if messageID != "" {
		msg.SetMessageIDWithValue(messageID)
	}

	if err := client.DialAndSendWithContext(ctx, msg); err != nil {
		return fmt.Errorf("mail: dial and send (mailbox %s): %w", fromAddr, err)
	}
	return nil
}
