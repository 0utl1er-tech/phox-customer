// Package campaign implements CampaignService — コールドメール一斉送信
// (Phase 27) のキャンペーン CRUD / ライフサイクル / 受信者一覧 / サプレッション。
//
// 実際の送信は internal/campaign.Worker (main.go の errgroup) が行う。
// このパッケージは状態遷移とスナップショット作成だけを担当する。
//
// RBAC:
//   - キャンペーンは会社スコープ。閲覧系は同じ会社のユーザーなら可。
//   - 作成/更新/開始/停止/削除はプール内全 Mailbox への editor 以上の
//     MailboxPermit が必要 (送信こそがセンシティブな操作)。
//   - 作成時は受信者が属する全 Book への editor 以上の Permit も必要。
package campaign

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	campaignpkg "github.com/0utl1er-tech/phox-customer/internal/campaign"
	"github.com/0utl1er-tech/phox-customer/internal/crypto"
	"github.com/0utl1er-tech/phox-customer/internal/mail"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type CampaignService struct {
	queries    *db.Queries
	dbPool     *pgxpool.Pool
	authorizer *auth.Authorizer
	sender     *mail.MailboxSender
	cipher     *crypto.Cipher
	tokenizer  *campaignpkg.Tokenizer
	publicBase string
}

// NewCampaignService creates the service. sender/cipher は SendTestEmail が、
// tokenizer/publicBase はテスト送信の配信停止 URL 生成が使う。
func NewCampaignService(
	queries *db.Queries,
	dbPool *pgxpool.Pool,
	sender *mail.MailboxSender,
	cipher *crypto.Cipher,
	tokenizer *campaignpkg.Tokenizer,
	publicBase string,
) *CampaignService {
	return &CampaignService{
		queries:    queries,
		dbPool:     dbPool,
		authorizer: auth.NewAuthorizer(queries),
		sender:     sender,
		cipher:     cipher,
		tokenizer:  tokenizer,
		publicBase: publicBase,
	}
}

// currentUser は認証済みユーザーの User 行を返す (会社スコープの起点)。
func (s *CampaignService) currentUser(ctx context.Context) (db.User, error) {
	token, err := auth.AuthorizeUser(ctx)
	if err != nil {
		return db.User{}, err
	}
	u, err := s.queries.GetUser(ctx, token.Subject())
	if err != nil {
		return db.User{}, connect.NewError(connect.CodePermissionDenied, errors.New("user not found"))
	}
	return u, nil
}

// getCampaignScoped は id のキャンペーンを取得し、呼び出しユーザーと同じ
// 会社であることを確認する。
func (s *CampaignService) getCampaignScoped(ctx context.Context, id string) (db.Campaign, db.User, error) {
	u, err := s.currentUser(ctx)
	if err != nil {
		return db.Campaign{}, db.User{}, err
	}
	cid, err := uuid.Parse(id)
	if err != nil {
		return db.Campaign{}, db.User{}, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid campaign id: %w", err))
	}
	c, err := s.queries.GetCampaign(ctx, cid)
	if err != nil {
		return db.Campaign{}, db.User{}, connect.NewError(connect.CodeNotFound, errors.New("campaign not found"))
	}
	if c.CompanyID != u.CompanyID {
		return db.Campaign{}, db.User{}, connect.NewError(connect.CodeNotFound, errors.New("campaign not found"))
	}
	return c, u, nil
}

// checkPoolPermission はキャンペーンの Mailbox プール全てに editor 以上を要求する。
func (s *CampaignService) checkPoolPermission(ctx context.Context, campaignID uuid.UUID) error {
	mailboxes, err := s.queries.ListCampaignMailboxes(ctx, campaignID)
	if err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("list campaign mailboxes: %w", err))
	}
	for _, mb := range mailboxes {
		if err := s.authorizer.CheckMailboxPermission(ctx, mb.ID, db.RoleEditor); err != nil {
			return err
		}
	}
	return nil
}
