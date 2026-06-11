package util

import (
	"fmt"

	"connectrpc.com/connect"
	"github.com/google/uuid"
)

// ParseUUID parses a request-supplied UUID string. Malformed input returns a
// CodeInvalidArgument connect error. リクエスト入力に uuid.MustParse を使うと
// 不正な ID 1 つで RPC が panic するため、ハンドラでは必ずこちらを使うこと。
func ParseUUID(field, value string) (uuid.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid %s: %w", field, err))
	}
	return id, nil
}
