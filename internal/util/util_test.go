package util_test

import (
	"testing"

	"connectrpc.com/connect"
	"github.com/0utl1er-tech/phox-customer/internal/util"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseUUID_Valid(t *testing.T) {
	want := uuid.New()
	got, err := util.ParseUUID("id", want.String())
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestParseUUID_Invalid(t *testing.T) {
	for _, in := range []string{"", "not-a-uuid", "12345"} {
		_, err := util.ParseUUID("book_id", in)
		require.Error(t, err, "input %q", in)
		assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
		assert.Contains(t, err.Error(), "book_id")
	}
}

func TestOptionalText(t *testing.T) {
	val := "hello"
	empty := ""

	got := util.OptionalText(&val)
	assert.True(t, got.Valid)
	assert.Equal(t, "hello", got.String)

	// nil と空文字列はどちらも「未指定」(Valid=false)
	assert.False(t, util.OptionalText(nil).Valid)
	assert.False(t, util.OptionalText(&empty).Valid)
}
