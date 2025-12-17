package user

import (
	"context"

	companyv1 "github.com/0utl1er-tech/phox-customer/gen/pb/company/v1"
	userv1 "github.com/0utl1er-tech/phox-customer/gen/pb/user/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
)

type UserService struct {
	queries *db.Queries
}

func NewUserService(queries *db.Queries) *UserService {
	return &UserService{
		queries: queries,
	}
}

// convertUserToProto converts a database User to a proto User
func (s *UserService) convertUserToProto(ctx context.Context, dbUser db.User) (*userv1.User, error) {
	company, err := s.queries.GetCompany(ctx, dbUser.CompanyID)
	if err != nil {
		return nil, err
	}

	return &userv1.User{
		Id:   dbUser.ID,
		Name: dbUser.Name,
		Company: &companyv1.Company{
			Id:   company.ID.String(),
			Name: company.Name,
		},
	}, nil
}
