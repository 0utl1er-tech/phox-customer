package activity

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	activityv1 "github.com/0utl1er-tech/phox-customer/gen/pb/activity/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/google/uuid"
)

// ListActivitiesByCustomerID は指定 Customer に紐づく Activity を時系列順で返す。
// `types` が空なら全 type を、非空なら指定 type だけを返す。
// 認可: 対応する Book に viewer 以上の権限が必要。
func (s *ActivityService) ListActivitiesByCustomerID(
	ctx context.Context,
	req *connect.Request[activityv1.ListActivitiesByCustomerIDRequest],
) (*connect.Response[activityv1.ListActivitiesByCustomerIDResponse], error) {
	customerID, err := uuid.Parse(req.Msg.CustomerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid customer_id: %w", err))
	}

	// Customer → Book を引いて permit チェック
	customer, err := s.queries.GetCustomer(ctx, customerID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("customer not found: %w", err))
	}

	if err := s.authorizer.CheckPermission(ctx, customer.BookID, db.RoleViewer); err != nil {
		return nil, err
	}

	rows, err := s.queries.ListActivitiesByCustomerID(ctx, db.ListActivitiesByCustomerIDParams{
		CustomerID: customerID,
		Types:      typesToStrings(req.Msg.Types),
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("list activities: %w", err))
	}

	activities := make([]*activityv1.Activity, 0, len(rows))
	for _, r := range rows {
		activities = append(activities, rowToProto(r))
	}

	return connect.NewResponse(&activityv1.ListActivitiesByCustomerIDResponse{
		Activities: activities,
	}), nil
}
