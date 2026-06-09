package graphql

import (
	"nft-market-backend/internal/repository"
	"nft-market-backend/internal/service"
)

// Resolver holds the dependencies needed by GraphQL resolvers.
type Resolver struct {
	OrderSvc       *service.OrderService
	CollectionRepo *repository.CollectionRepo
	OrderRepo      *repository.OrderRepo
}
