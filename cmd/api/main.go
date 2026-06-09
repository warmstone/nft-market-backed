// @title           NFT Market API
// @version         1.0
// @description     EIP-712 signed order DEX backend. Submit signed orders, browse the order book, query collections and stats.
// @host            localhost:8080
// @BasePath        /api/v1
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization

package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"nft-market-backend/internal/config"
	"nft-market-backend/internal/graphql"
	"nft-market-backend/internal/handler"
	logpkg "nft-market-backend/internal/log"
	"nft-market-backend/internal/middleware"
	"nft-market-backend/internal/repository"
	"nft-market-backend/internal/rpc"
	"nft-market-backend/internal/service"
	"nft-market-backend/internal/watcher"
	"nft-market-backend/internal/ws"

	_ "nft-market-backend/docs"

	"github.com/ethereum/go-ethereum/common"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/lib/pq"
	"go.uber.org/zap"
)

func main() {
	logpkg.Init(os.Getenv("LOG_LEVEL"))
	defer logpkg.Sync()

	cfg, err := config.Load("config/config.yaml")
	if err != nil {
		logpkg.Logger.Fatal("config load failed", zap.Error(err))
	}

	// Database.
	db, err := sql.Open("postgres", cfg.Database.DSN())
	if err != nil {
		logpkg.Logger.Fatal("database open failed", zap.Error(err))
	}
	if err := db.Ping(); err != nil {
		logpkg.Logger.Fatal("database ping failed", zap.Error(err))
	}
	logpkg.Logger.Info("database connected")

	// Run migrations.
	if cfg.Migration.Enabled {
		logpkg.Logger.Info("running database migrations")
		m, err := migrate.New("file://migrations", cfg.Database.MigrationURL())
		if err != nil {
			logpkg.Logger.Fatal("migration init failed", zap.Error(err))
		}
		if err := m.Up(); err != nil && err != migrate.ErrNoChange {
			logpkg.Logger.Fatal("migration failed", zap.Error(err))
		}
		logpkg.Logger.Info("migrations complete")
	}

	// Redis.
	cacheSvc, err := service.NewCacheService(cfg.Redis.Addr)
	if err != nil {
		logpkg.Logger.Fatal("redis connect failed", zap.Error(err))
	}
	logpkg.Logger.Info("redis connected")

	// RPC client.
	rpcClient, err := rpc.NewClient(cfg.Ethereum.RPCURL, cfg.Ethereum.ChainID)
	if err != nil {
		logpkg.Logger.Fatal("rpc client failed", zap.Error(err))
	}
	defer rpcClient.Close()

	rpcClient.SetContractAddresses(
		common.HexToAddress(cfg.Ethereum.ExchangeAddress),
		common.HexToAddress(cfg.Ethereum.ProtocolManagerAddress),
		common.HexToAddress(cfg.Ethereum.CollectionManagerAddress),
		common.HexToAddress(cfg.Ethereum.RoyaltyManagerAddress),
	)

	// Repositories.
	orderRepo := repository.NewOrderRepo(db)
	orderRepo.ChainID = cfg.Ethereum.ChainID

	eventRepo := repository.NewEventRepo(db)

	collectionRepo := repository.NewCollectionRepo(db)
	collectionRepo.ChainID = cfg.Ethereum.ChainID

	// WebSocket hub.
	hub := ws.NewHub(cfg.Server.AllowedOrigins)
	go hub.Run()

	// Services.
	sigSvc := service.NewSignatureService(cfg.Ethereum.ChainID, cfg.Ethereum.ExchangeAddress)

	// Auth service.
	authSvc := service.NewAuthService(
		cacheSvc,
		cfg.Auth.JWTSecret,
		cfg.Auth.JWTExpiry,
		cfg.Auth.ChallengeTTL,
	)
	authH := handler.NewAuthHandler(authSvc)

	orderSvc := service.NewOrderService(orderRepo, collectionRepo, sigSvc, cfg.Ethereum.ChainID)
	eventSvc := service.NewEventService(orderRepo, collectionRepo, cacheSvc, hub)

	metadataSvc := service.NewMetadataService(
		collectionRepo,
		rpcClient.CallContract,
		"https://ipfs.io",
	)
	scheduler := service.NewScheduler(orderRepo, collectionRepo, metadataSvc)

	// Event watcher.
	w := watcher.NewWatcher(
		rpcClient,
		eventRepo,
		eventSvc,
		cfg.Ethereum.ChainID,
		uint64(cfg.Ethereum.ConfirmationBlocks),
	)

	// Handlers.
	orderH := handler.NewOrderHandler(orderSvc, metadataSvc, hub)
	collectionH := handler.NewCollectionHandler(collectionRepo, orderRepo)
	wsH := handler.NewWSHandler(hub)
	// GraphQL resolver and handler.
	gqlResolver := &graphql.Resolver{
		OrderSvc:       orderSvc,
		CollectionRepo: collectionRepo,
		OrderRepo:      orderRepo,
	}
	graphqlH := handler.NewGraphQLHandler(gqlResolver)

	// Router.
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(middleware.RequestID())
	router.Use(middleware.AccessLog())
	router.Use(middleware.Metrics())
	router.Use(middleware.CORS(cfg.Server.AllowedOrigins))
	router.Use(middleware.RateLimit(10, 20))

	// Body size limit: 1MB
	router.MaxMultipartMemory = 1 << 20

	// Health checks (no auth).
	healthH := handler.NewHealthHandler(db, cacheSvc.Client(), rpcClient)
	router.GET("/metrics", gin.WrapH(promhttp.Handler()))
	router.GET("/health", healthH.Health)
	router.GET("/ready", healthH.Ready)

	// Swagger UI.
	router.GET("/api/v1/docs/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// Auth routes (no auth required).
	auth := router.Group("/api/v1/auth")
	{
		auth.GET("/challenge", authH.Challenge)
		auth.POST("/login", authH.Login)
	}

	api := router.Group("/api/v1")
	// Public routes (no auth).
	{
		api.GET("/orders", orderH.List)
		api.GET("/orders/best", orderH.Best)
		api.GET("/orders/:hash", orderH.Get)
		api.GET("/collections", collectionH.List)
		api.GET("/collections/:address", collectionH.Get)
		api.GET("/stats", collectionH.GlobalStats)
		api.GET("/stats/:collection", collectionH.CollectionStats)
		api.GET("/graphql", graphqlH.Playground)
	}

	// Protected routes (auth required).
	protected := router.Group("/api/v1")
	protected.Use(middleware.Auth(authSvc))
	{
		protected.POST("/orders", orderH.Submit)
		protected.GET("/users/:address/orders", orderH.UserOrders)
		protected.POST("/graphql", graphqlH.Handle)
	}

	router.GET("/ws/orders", wsH.Handle)

	// Background goroutines.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go w.Run(ctx)
	go metadataSvc.Run(ctx)
	go scheduler.Run(ctx)

	// HTTP server.
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Server.Port),
		Handler: router,
	}

	go func() {
		logpkg.Logger.Info("api listening", zap.Int("port", cfg.Server.Port))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logpkg.Logger.Fatal("server failed", zap.Error(err))
		}
	}()

	// Graceful shutdown.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logpkg.Logger.Info("shutting down...")
	cancel()
	srv.Shutdown(context.Background())
	db.Close()
}
