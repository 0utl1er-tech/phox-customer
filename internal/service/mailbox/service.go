// Package mailbox implements MailboxService — management of the real mailu
// mailboxes Phox owns, with Book/Permit-style RBAC (owner/editor/viewer).
// Passwords are AES-GCM encrypted at rest via the injected crypto.Cipher and
// never returned in any response.
package mailbox

import (
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/crypto"
	"github.com/0utl1er-tech/phox-customer/internal/mailu"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
)

type MailboxService struct {
	queries    *db.Queries
	authorizer *auth.Authorizer
	cipher     *crypto.Cipher // メールボックスパスワードの暗号化/復号

	// Phase 25/D: mailu 管理 API クライアント。non-nil のとき CreateMailbox は
	// mailu アカウントを自動作成し、DeleteMailbox は削除する。nil なら
	// 「既存アカウント登録」モード (mailu 側は人が作る)。
	provisioner *mailu.Client
}

// NewMailboxService creates the service. cipher must be non-nil (built from
// MAILBOX_SECRET_KEY at boot) so passwords can be encrypted. provisioner may
// be nil (register-existing mode).
func NewMailboxService(queries *db.Queries, cipher *crypto.Cipher, provisioner *mailu.Client) *MailboxService {
	return &MailboxService{
		queries:     queries,
		authorizer:  auth.NewAuthorizer(queries),
		cipher:      cipher,
		provisioner: provisioner,
	}
}
