// Package mailbox implements MailboxService — management of the real mailu
// mailboxes Phox owns, with Book/Permit-style RBAC (owner/editor/viewer).
// Passwords are AES-GCM encrypted at rest via the injected crypto.Cipher and
// never returned in any response.
package mailbox

import (
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/crypto"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
)

type MailboxService struct {
	queries    *db.Queries
	authorizer *auth.Authorizer
	cipher     *crypto.Cipher // メールボックスパスワードの暗号化/復号
}

// NewMailboxService creates the service. cipher must be non-nil (built from
// MAILBOX_SECRET_KEY at boot) so passwords can be encrypted.
func NewMailboxService(queries *db.Queries, cipher *crypto.Cipher) *MailboxService {
	return &MailboxService{
		queries:    queries,
		authorizer: auth.NewAuthorizer(queries),
		cipher:     cipher,
	}
}
