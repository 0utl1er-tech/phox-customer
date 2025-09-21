package main

import (
	"context"
	"net/http"
	"os"
	"syscall"

	"github.com/0utl1er-tech/phox-customer/gen/pb/book/v1/bookv1connect"
	contactv1connect "github.com/0utl1er-tech/phox-customer/gen/pb/contact/v1/contactv1connect"
	customerv1connect "github.com/0utl1er-tech/phox-customer/gen/pb/customer/v1/customerv1connect"
	permitv1connect "github.com/0utl1er-tech/phox-customer/gen/pb/permit/v1/permitv1connect"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/service/book"
	"github.com/0utl1er-tech/phox-customer/internal/service/contact"
	"github.com/0utl1er-tech/phox-customer/internal/service/customer"
	"github.com/0utl1er-tech/phox-customer/internal/service/permit"
	"github.com/0utl1er-tech/phox-customer/internal/util"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"golang.org/x/sync/errgroup"
)

var interruptSignals = []os.Signal{
	os.Interrupt,
	syscall.SIGTERM,
	syscall.SIGINT,
}

func main() {
	cfg, err := util.LoadConfig(".")
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load config")
	}

	connPool, err := pgxpool.New(context.Background(), cfg.DBSource)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create connection pool")
	}

	queries := db.New(connPool)
	customerService := customer.NewCustomerService(queries)
	bookService := book.NewBookService(queries)
	permitService := permit.NewPermitService(queries)
	contactService := contact.NewContactService(queries)
	// HTTPサーバーの設定
	mux := http.NewServeMux()

	// Connect-Goハンドラーを登録（認証interceptor付き）
	customerPath, customerHandler := customerv1connect.NewCustomerServiceHandler(customerService)
	bookPath, bookHandler := bookv1connect.NewBookServiceHandler(bookService)
	permitPath, permitHandler := permitv1connect.NewPermitServiceHandler(permitService)
	contactPath, contactHandler := contactv1connect.NewContactServiceHandler(contactService)

	mux.Handle(customerPath, customerHandler)
	mux.Handle(bookPath, bookHandler)
	mux.Handle(permitPath, permitHandler)
	mux.Handle(contactPath, contactHandler)

	// HTTP/2対応のサーバーを作成
	server := &http.Server{
		Addr:    cfg.ConnectServerAddress,
		Handler: h2c.NewHandler(mux, &http2.Server{}),
	}

	// サーバー起動とGraceful Shutdown
	waitGroup, ctx := errgroup.WithContext(context.Background())

	waitGroup.Go(func() error {
		log.Info().Msgf("Start Connect-Go server at %s", server.Addr)
		err := server.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			log.Error().Err(err).Msg("Connect-Go server failed to serve")
			return err
		}
		return nil
	})

	waitGroup.Go(func() error {
		<-ctx.Done()
		log.Info().Msg("Graceful shutdown Connect-Go server")
		err := server.Shutdown(context.Background())
		if err != nil {
			log.Error().Err(err).Msg("Failed to shutdown server gracefully")
			return err
		}
		log.Info().Msg("Connect-Go server is stopped")
		return nil
	})

	err = waitGroup.Wait()
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to wait")
	}
}
