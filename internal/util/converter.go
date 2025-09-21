package util

import (
	permitv1 "github.com/0utl1er-tech/phox-customer/gen/pb/permit/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
)

// convertDBRoleToProtoRole converts sqlc Role to proto Role
func ConvertDBRoleToProtoRole(dbRole db.Role) permitv1.Role {
	switch dbRole {
	case db.RoleViewer:
		return permitv1.Role_ROLE_VIEWER
	case db.RoleEditor:
		return permitv1.Role_ROLE_EDITOR
	case db.RoleOwner:
		return permitv1.Role_ROLE_OWNER
	default:
		return permitv1.Role_ROLE_UNSPECIFIED
	}
}

// convertProtoRoleToDBRole converts proto Role to sqlc Role
func ConvertProtoRoleToDBRole(protoRole permitv1.Role) db.Role {
	switch protoRole {
	case permitv1.Role_ROLE_VIEWER:
		return db.RoleViewer
	case permitv1.Role_ROLE_EDITOR:
		return db.RoleEditor
	case permitv1.Role_ROLE_OWNER:
		return db.RoleOwner
	default:
		return db.RoleViewer // デフォルト値としてVIEWERを返す
	}
}
