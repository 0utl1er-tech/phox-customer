// Package activity は Activity (call / email_sent / email_received) の CRUD を
// 提供する Connect-Go サービス。既存の CallService を段階的に置き換える。
package activity

import (
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/crypto"
	"github.com/0utl1er-tech/phox-customer/internal/mail"
	"github.com/0utl1er-tech/phox-customer/internal/recording"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
)

// ActivityService は ActivityServiceHandler を実装する。
// mailClient / recordingSvc は nil 許容 (個別機能が有効な環境でのみ注入される)。
type ActivityService struct {
	queries      *db.Queries
	authorizer   *auth.Authorizer
	mailClient   *mail.SMTPClient
	recordingSvc *recording.Service

	// Phase 25: 実メールボックス送信。両方 non-nil のときだけ
	// CreateActivityEmailSent の mailbox_id 指定が使える。
	mailboxSender *mail.MailboxSender
	mailboxCipher *crypto.Cipher
}

// NewActivityService は必要な依存を組み立てて ActivityService を返す。
// `mailClient` が nil の場合、`CreateActivityEmailSent` は CodeUnavailable を返す。
// `recordingSvc` が nil または disabled の場合、`GetActivityRecording` は CodeUnavailable を返す。
func NewActivityService(queries *db.Queries, mailClient *mail.SMTPClient, recordingSvc *recording.Service) *ActivityService {
	return &ActivityService{
		queries:      queries,
		authorizer:   auth.NewAuthorizer(queries),
		mailClient:   mailClient,
		recordingSvc: recordingSvc,
	}
}

// WithMailboxSending は実メールボックス送信 (Phase 25) を有効化する。
// sender は共有 mailu 接続、cipher はメールボックスパスワードの復号に使う
// (MailboxService と同じ鍵)。既存の呼び出し/テストを壊さない chainable setter。
func (s *ActivityService) WithMailboxSending(sender *mail.MailboxSender, cipher *crypto.Cipher) *ActivityService {
	s.mailboxSender = sender
	s.mailboxCipher = cipher
	return s
}
