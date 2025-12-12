package book

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"

	"github.com/0utl1er-tech/phox-customer/internal/util"
	"github.com/jackc/pgx/v5/pgxpool"

	bookv1 "github.com/0utl1er-tech/phox-customer/gen/pb/book/v1"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
)

func newTestBookService(t *testing.T) *BookService {
	cfg := util.Config{
		DBSource: "postgres://root:secret@localhost:5432/phox-customer?sslmode=disable",
	}
	connPool, err := pgxpool.New(context.Background(), cfg.DBSource)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create connection pool")
	}
	queries := db.New(connPool)
	bookService := NewBookService(queries)

	return bookService
}

func TestBook(t *testing.T) {
	bookService := newTestBookService(t)
	createReq := connect.NewRequest(&bookv1.CreateBookRequest{
		Name: "テスト顧客リスト",
	})
	createReq.Header().Set("X-User-ID", "joe@0utl1er.tech")
	createdBook, err := bookService.CreateBook(context.Background(), createReq)
	assert.NoError(t, err)
	assert.NotNil(t, createdBook)

	updateReq := connect.NewRequest(&bookv1.UpdateBookRequest{
		Id:   createdBook.Msg.Id,
		Name: "テスト顧客リスト2",
	})
	updateReq.Header().Set("X-User-ID", "joe@0utl1er.tech")

	updatedBook, err := bookService.UpdateBook(context.Background(), updateReq)
	assert.NoError(t, err)
	assert.NotNil(t, updatedBook)
	assert.Equal(t, updatedBook.Msg.UpdatedBook.Name, "テスト顧客リスト2")

	deleteReq := connect.NewRequest(&bookv1.DeleteBookRequest{
		Id: updatedBook.Msg.UpdatedBook.Id,
	})
	deleteReq.Header().Set("X-User-ID", "joe@0utl1er.tech")

	deletedBook, err := bookService.DeleteBook(context.Background(), deleteReq)
	assert.NoError(t, err)
	assert.NotNil(t, deletedBook)
}
