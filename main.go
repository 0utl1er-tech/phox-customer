package main

import (
	"context"
	"net/http"
	"os"
	"syscall"

	customerv1connect "github.com/0utl1er-tech/phox-customer/gen/pb/customer/v1/customerv1connect"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/service"
	"github.com/0utl1er-tech/phox-customer/internal/service/book"
	"github.com/0utl1er-tech/phox-customer/internal/service/customer"
	"github.com/0utl1er-tech/phox-customer/internal/util"
	"github.com/bufbuild/connect-go"
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
	customerService := customer.NewService(queries)
	bookService := book.NewService(queries)

	waitGroup, ctx := errgroup.WithContext(context.Background())
	runConnectServer(ctx, waitGroup, customerService, bookService, &cfg)

	err = waitGroup.Wait()
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to wait")
	}
}

func runConnectServer(
	ctx context.Context,
	waitGroup *errgroup.Group,
	customerService customer.Service,
	bookService book.Service,
	cfg *util.Config,
) {
	mux := http.NewServeMux()

	// Connect-Goハンドラーを登録（認証interceptor付き）
	path, handler := customerv1connect.NewCustomerServiceHandler(
		customerService,
		connect.WithInterceptors(service.AuthInterceptor()),
	)

	mux.Handle(path, handler)

	// HTTP/2対応のサーバーを作成
	server := &http.Server{
		Addr:    cfg.ConnectServerAddress,
		Handler: h2c.NewHandler(mux, &http2.Server{}),
	}

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
}
