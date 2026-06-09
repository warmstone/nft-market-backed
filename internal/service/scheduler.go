package service

import (
	"context"
	"log"
	"time"

	"nft-market-backend/internal/repository"
)

// Scheduler runs periodic background tasks.
type Scheduler struct {
	orderRepo      *repository.OrderRepo
	collectionRepo *repository.CollectionRepo
	metadataSvc    *MetadataService
}

// NewScheduler creates a Scheduler.
func NewScheduler(
	orderRepo *repository.OrderRepo,
	collectionRepo *repository.CollectionRepo,
	metadataSvc *MetadataService,
) *Scheduler {
	return &Scheduler{
		orderRepo:      orderRepo,
		collectionRepo: collectionRepo,
		metadataSvc:    metadataSvc,
	}
}

// Run starts the expire orders loop and metadata refresh loop.
func (s *Scheduler) Run(ctx context.Context) {
	go s.expireOrdersLoop(ctx)
	go s.metadataRefreshLoop(ctx)
}

func (s *Scheduler) expireOrdersLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := s.orderRepo.ExpireOrders()
			if err != nil {
				log.Printf("scheduler: expire orders: %v", err)
			} else if n > 0 {
				log.Printf("scheduler: expired %d orders", n)
			}
		}
	}
}

func (s *Scheduler) metadataRefreshLoop(ctx context.Context) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.metadataSvc.RefreshStale(ctx); err != nil {
				log.Printf("scheduler: metadata refresh: %v", err)
			}
		}
	}
}
