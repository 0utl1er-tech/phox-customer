// Package googleoauth は Google 連携ステータスと切断を扱う Connect サービス。
// OAuth 自体の HTTP フローは internal/oauth パッケージが別途管理する。
package googleoauth

import (
	"context"

	"connectrpc.com/connect"
	googleoauthv1 "github.com/0utl1er-tech/phox-customer/gen/pb/googleoauth/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
	"google.golang.org/protobuf/types/known/emptypb"
)

type GoogleOAuthService struct {
	queries *db.Queries
}

func NewGoogleOAuthService(queries *db.Queries) *GoogleOAuthService {
	return &GoogleOAuthService{queries: queries}
}

func (s *GoogleOAuthService) GetGoogleConnectionStatus(
	ctx context.Context,
	req *connect.Request[googleoauthv1.GetGoogleConnectionStatusRequest],
) (*connect.Response[googleoauthv1.GetGoogleConnectionStatusResponse], error) {
	token, err := auth.AuthorizeUser(ctx)
	if err != nil {
		return nil, err
	}
	userID := token.Subject()

	row, err := s.queries.GetUserGoogleToken(ctx, userID)
	if err != nil {
		return connect.NewResponse(&googleoauthv1.GetGoogleConnectionStatusResponse{
			Connected: false,
		}), nil
	}
	return connect.NewResponse(&googleoauthv1.GetGoogleConnectionStatusResponse{
		Connected:   true,
		GoogleEmail: row.GoogleEmail,
		Scopes:      row.Scopes,
	}), nil
}

func (s *GoogleOAuthService) DisconnectGoogle(
	ctx context.Context,
	req *connect.Request[googleoauthv1.DisconnectGoogleRequest],
) (*connect.Response[emptypb.Empty], error) {
	token, err := auth.AuthorizeUser(ctx)
	if err != nil {
		return nil, err
	}
	userID := token.Subject()
	if err := s.queries.DeleteUserGoogleToken(ctx, userID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}
