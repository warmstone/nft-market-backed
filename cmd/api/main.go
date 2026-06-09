package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"nft-market-backend/internal/config"
	"nft-market-backend/internal/handler"
	"nft-market-backend/internal/middleware"
	"nft-market-backend/internal/repository"
	"nft-market-backend/internal/rpc"
	"nft-market-backend/internal/service"
	"nft-market-backend/internal/watcher"
	"nft-market-backend/internal/ws"

	"github.com/ethereum/go-ethereum/common"
	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
)

func main() {
	cfg, err := config.Load("config/config.yaml")
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	// Database.
	db, err := sql.Open("postgres", cfg.Database.DSN())
	if err != nil {
		log.Fatalf("database open: %v", err)
	}
	if err := db.Ping(); err != nil {
		log.Fatalf("database ping: %v", err)
	}
	log.Println("database connected")

	// Redis.
	cacheSvc, err := service.NewCacheService(cfg.Redis.Addr)
	if err != nil {
		log.Fatalf("redis: %v", err)
	}
	log.Println("redis connected")

	// RPC client.
	rpcClient, err := rpc.NewClient(cfg.Ethereum.RPCURL, cfg.Ethereum.ChainID)
	if err != nil {
		log.Fatalf("rpc client: %v", err)
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
	hub := ws.NewHub()
	go hub.Run()

	// Services.
	sigSvc := service.NewSignatureService(cfg.Ethereum.ChainID, cfg.Ethereum.ExchangeAddress)
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
	orderH := handler.NewOrderHandler(orderSvc, metadataSvc)
	collectionH := handler.NewCollectionHandler(collectionRepo, orderRepo)
	wsH := handler.NewWSHandler(hub)
	graphqlH := handler.NewGraphQLHandler()

	// Router.
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(middleware.CORS())
	router.Use(middleware.RateLimit(10, 20))

	api := router.Group("/api/v1")
	{
		api.POST("/orders", orderH.Submit)
		api.GET("/orders", orderH.List)
		api.GET("/orders/best", orderH.Best)
		api.GET("/orders/:hash", orderH.Get)

		api.GET("/collections", collectionH.List)
		api.GET("/collections/:address", collectionH.Get)

		api.GET("/users/:address/orders", orderH.UserOrders)

		api.GET("/stats", collectionH.GlobalStats)
		api.GET("/stats/:collection", collectionH.CollectionStats)

		api.POST("/graphql", graphqlH.Handle)
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
		log.Printf("api listening on :%d", cfg.Server.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()

	// Graceful shutdown.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("shutting down...")
	cancel()
	srv.Shutdown(context.Background())
	db.Close()
}
