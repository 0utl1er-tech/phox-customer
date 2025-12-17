package user

import (
	"context"

	companyv1 "github.com/0utl1er-tech/phox-customer/gen/pb/company/v1"
	userv1 "github.com/0utl1er-tech/phox-customer/gen/pb/user/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/firebaseadmin"
	"github.com/jackc/pgx/v5/pgxpool"
)

type UserService struct {
	queries      *db.Queries
	firebaseAuth *firebaseadmin.Client
	dbPool       *pgxpool.Pool
}

func NewUserService(queries *db.Queries, firebaseAuth *firebaseadmin.Client, dbPool *pgxpool.Pool) *UserService {
	return &UserService{
		queries:      queries,
		firebaseAuth: firebaseAuth,
		dbPool:       dbPool,
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
