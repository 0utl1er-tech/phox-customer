package main

import (
	"context"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"github.com/0utl1er-tech/phox-customer/gen/pb/book/v1/bookv1connect"
	callv1connect "github.com/0utl1er-tech/phox-customer/gen/pb/call/v1/callv1connect"
	contactv1connect "github.com/0utl1er-tech/phox-customer/gen/pb/contact/v1/contactv1connect"
	customerv1connect "github.com/0utl1er-tech/phox-customer/gen/pb/customer/v1/customerv1connect"
	permitv1connect "github.com/0utl1er-tech/phox-customer/gen/pb/permit/v1/permitv1connect"
	statusv1connect "github.com/0utl1er-tech/phox-customer/gen/pb/status/v1/statusv1connect"
	userv1connect "github.com/0utl1er-tech/phox-customer/gen/pb/user/v1/userv1connect"
	db "github.com/0utl1er-tech/phox-customer/gen/sqlc"
	"github.com/0utl1er-tech/phox-customer/internal/firebaseadmin"
	"github.com/0utl1er-tech/phox-customer/internal/service/auth"
	"github.com/0utl1er-tech/phox-customer/internal/service/book"
	"github.com/0utl1er-tech/phox-customer/internal/service/call"
	"github.com/0utl1er-tech/phox-customer/internal/service/contact"
	"github.com/0utl1er-tech/phox-customer/internal/service/customer"
	"github.com/0utl1er-tech/phox-customer/internal/service/permit"
	"github.com/0utl1er-tech/phox-customer/internal/service/status"
	"github.com/0utl1er-tech/phox-customer/internal/service/user"
	"github.com/0utl1er-tech/phox-customer/internal/util"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"golang.org/x/sync/errgroup"
)

// corsMiddleware adds CORS headers to allow cross-origin requests
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// すべてのオリジンからのリクエストを許可
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		} else {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Connect-Protocol-Version, Connect-Timeout-Ms")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Max-Age", "86400")

		// Handle preflight requests
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
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
	defer connPool.Close()

	// Test database connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := connPool.Ping(ctx); err != nil {
		log.Fatal().Err(err).Msg("Failed to ping database - connection not established")
	}

	log.Info().Msg("Database connection established successfully")

	queries := db.New(connPool)

	// Initialize Firebase Admin client (optional - if credentials file is configured)
	var firebaseClient *firebaseadmin.Client
	if cfg.FirebaseAdminCredentials != "" {
		var err error
		firebaseClient, err = firebaseadmin.NewClient(context.Background(), cfg.FirebaseAdminCredentials)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to initialize Firebase Admin client - CreateCompanyUser will not work")
		} else {
			log.Info().Msg("Firebase Admin client initialized successfully")
		}
	} else {
		log.Warn().Msg("Firebase Admin credentials not configured - CreateCompanyUser will not work")
	}

	// Create interceptor
	authInterceptor := auth.NewAuthInterceptor(context.Background(), queries, cfg)
	interceptors := connect.WithInterceptors(authInterceptor)

	// Create services
	customerService := customer.NewCustomerService(queries)
	bookService := book.NewBookService(queries)
	permitService := permit.NewPermitService(queries)
	contactService := contact.NewContactService(queries)
	statusService := status.NewStatusService(queries)
	callService := call.NewCallService(queries)
	userService := user.NewUserService(queries, firebaseClient, connPool)

	// HTTPサーバーの設定
	mux := http.NewServeMux()

	// Connect-Goハンドラーを登録（ミドルウェア付き）
	customerPath, customerHandler := customerv1connect.NewCustomerServiceHandler(customerService, interceptors)
	bookPath, bookHandler := bookv1connect.NewBookServiceHandler(bookService, interceptors)
	permitPath, permitHandler := permitv1connect.NewPermitServiceHandler(permitService, interceptors)
	contactPath, contactHandler := contactv1connect.NewContactServiceHandler(contactService, interceptors)
	statusPath, statusHandler := statusv1connect.NewStatusServiceHandler(statusService, interceptors)
	callPath, callHandler := callv1connect.NewCallServiceHandler(callService, interceptors)
	userPath, userHandler := userv1connect.NewUserServiceHandler(userService, interceptors)

	mux.Handle(customerPath, customerHandler)
	mux.Handle(bookPath, bookHandler)
	mux.Handle(permitPath, permitHandler)
	mux.Handle(contactPath, contactHandler)
	mux.Handle(statusPath, statusHandler)
	mux.Handle(callPath, callHandler)
	mux.Handle(userPath, userHandler)

	// CORSミドルウェアを適用
	corsHandler := corsMiddleware(mux)

	// HTTP/2対応のサーバーを作成
	server := &http.Server{
		Addr:    cfg.ConnectServerAddress,
		Handler: h2c.NewHandler(corsHandler, &http2.Server{}),
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
