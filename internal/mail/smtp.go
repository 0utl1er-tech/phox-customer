// Package mail は SMTP 送信と IMAP 取込みの薄いラッパを提供する。
//
// SMTP は wneessen/go-mail を使い、TLS モードは 3 種類から選べる:
//   - "none":     MailHog / 非暗号 SMTP (port 1025 など)
//   - "implicit": SMTPS / port 465 (mailu の本番ポリシー)
//   - "starttls": STARTTLS / port 587 (他のサーバー向け)
//
// mailu は STARTTLS 非推奨なので、本番設定は必ず "implicit" + 465 で行う。
package mail

import (
	"context"
	"errors"
	"fmt"

	gomail "github.com/wneessen/go-mail"
)

// ErrNotConfigured は SMTP client が nil あるいは未設定のときに返される sentinel。
var ErrNotConfigured = errors.New("mail: SMTP client not configured")

// Config holds the subset of util.Config the mail package needs.
// main.go で util.Config から詰め替えて渡す。
type Config struct {
	Host        string
	Port        int
	Username    string
	Password    string
	DefaultFrom string
	TLSMode     string // "none" | "implicit" | "starttls"
}

// SMTPClient wraps gomail.Client with TLS-mode-specific initialization.
type SMTPClient struct {
	client      *gomail.Client
	defaultFrom string
}

// NewSMTPClient returns nil, nil when cfg.Host is empty (degraded mode).
// Returns error when cfg is invalid or the underlying client fails to build.
func NewSMTPClient(cfg Config) (*SMTPClient, error) {
	if cfg.Host == "" {
		return nil, nil
	}
	if cfg.Port == 0 {
		return nil, errors.New("mail: SMTP_PORT is required when SMTP_HOST is set")
	}

	opts := []gomail.Option{
		gomail.WithPort(cfg.Port),
	}

	// TLS mode の分岐
	switch cfg.TLSMode {
	case "", "none":
		opts = append(opts, gomail.WithTLSPolicy(gomail.NoTLS))
	case "implicit":
		// SMTPS: TCP 接続自体を TLS で確立 (mailu 本番ポリシー)
		opts = append(opts,
			gomail.WithSSL(),
			gomail.WithTLSPolicy(gomail.TLSMandatory),
		)
	case "starttls":
		// STARTTLS: 平文で始めて TLS にアップグレード
		opts = append(opts, gomail.WithTLSPolicy(gomail.TLSMandatory))
	default:
		return nil, fmt.Errorf("mail: unknown SMTP_TLS_MODE=%q (expected none|implicit|starttls)", cfg.TLSMode)
	}

	// 認証: username/password が揃っていれば LOGIN + PLAIN で試行
	if cfg.Username != "" {
		opts = append(opts,
			gomail.WithSMTPAuth(gomail.SMTPAuthPlain),
			gomail.WithUsername(cfg.Username),
			gomail.WithPassword(cfg.Password),
		)
	}

	client, err := gomail.NewClient(cfg.Host, opts...)
	if err != nil {
		return nil, fmt.Errorf("mail: new client: %w", err)
	}

	return &SMTPClient{
		client:      client,
		defaultFrom: cfg.DefaultFrom,
	}, nil
}

// Send builds a RFC822 message and dials out to deliver it.
//
// `from` is the header-level sender (= CRM ユーザー個人の email). SMTP 認証は
// Config で指定されたサービスアカウントで行うため、mailu 側のサービスアカウント
// が任意 From ヘッダでの送信を許可されている必要がある (allow_spoofing=true)。
//
// Reply-To は自動的に `defaultFrom` (= サービスアカウント phox@...) に設定される。
// こうすることで顧客が「返信」を押した時にシステムの INBOX に届き、IMAP worker
// が取込める。顧客には From: joe@... (個人) が見えるが、返信先は phox@... になる。
// これは Salesforce / HubSpot 等の CRM 標準パターン。
//
// `messageID` は呼び出し側 (CreateActivityEmailSent) が UUID ベースで採番した
// 値を渡し、dedup のキーとして DB の `message_id` に保存される。
func (s *SMTPClient) Send(
	ctx context.Context,
	from, fromName, to string,
	cc []string,
	subject, body, messageID string,
) error {
	if s == nil || s.client == nil {
		return ErrNotConfigured
	}
	if from == "" {
		from = s.defaultFrom
	}
	if from == "" {
		return errors.New("mail: from address is empty")
	}

	msg := gomail.NewMsg()
	// fromName があれば `"黒羽晟" <joe@0utl1er.tech>` 形式、無ければアドレスのみ
	if fromName != "" {
		if err := msg.FromFormat(fromName, from); err != nil {
			return fmt.Errorf("mail: set from format: %w", err)
		}
	} else {
		if err := msg.From(from); err != nil {
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

	// Reply-To: サービスアカウント (= IMAP worker が polling する INBOX の持ち主)。
	// from != defaultFrom の場合のみ設定 (from == defaultFrom ならそもそも不要)。
	if s.defaultFrom != "" && from != s.defaultFrom {
		if err := msg.ReplyTo(s.defaultFrom); err != nil {
			return fmt.Errorf("mail: set reply-to: %w", err)
		}
	}

	msg.Subject(subject)
	msg.SetBodyString(gomail.TypeTextPlain, body)
	if messageID != "" {
		msg.SetMessageIDWithValue(messageID)
	}

	if err := s.client.DialAndSendWithContext(ctx, msg); err != nil {
		return fmt.Errorf("mail: dial and send: %w", err)
	}
	return nil
}
