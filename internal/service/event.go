package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"

	"nft-market-backend/internal/domain"
	"nft-market-backend/internal/repository"
	"nft-market-backend/internal/ws"
)

// EventService handles chain events and dispatches state transitions.
type EventService struct {
	orderRepo      *repository.OrderRepo
	collectionRepo *repository.CollectionRepo
	cache          *CacheService
	hub            *ws.Hub
}

// NewEventService creates an EventService.
func NewEventService(
	orderRepo *repository.OrderRepo,
	collectionRepo *repository.CollectionRepo,
	cache *CacheService,
	hub *ws.Hub,
) *EventService {
	return &EventService{
		orderRepo:      orderRepo,
		collectionRepo: collectionRepo,
		cache:          cache,
		hub:            hub,
	}
}

// Handle processes a single contract event and updates state accordingly.
func (s *EventService) Handle(event *domain.ContractEvent) error {
	switch event.EventName {
	case domain.EventOrderFulfilled:
		return s.handleOrderFulfilled(event)
	case domain.EventOrderCancelled:
		return s.handleOrderCancelled(event)
	case domain.EventCounterIncremented:
		return s.handleCounterIncremented(event)
	case domain.EventCollectionUpdated:
		return s.handleCollectionUpdated(event)
	default:
		return nil
	}
}

func (s *EventService) handleOrderFulfilled(event *domain.ContractEvent) error {
	var data domain.OrderFulfilledData
	if err := json.Unmarshal(event.EventData, &data); err != nil {
		return fmt.Errorf("unmarshal OrderFulfilled: %w", err)
	}

	if err := s.orderRepo.UpdateStatus(data.OrderHash, domain.Filled); err != nil {
		return fmt.Errorf("update order status: %w", err)
	}

	order, _ := s.orderRepo.FindByHash(data.OrderHash)
	if order != nil {
		_ = s.cache.InvalidateOrders(context.Background(), order.Collection)
		s.broadcast(order.Collection, "order:filled", map[string]string{
			"orderHash":  data.OrderHash,
			"txHash":     event.TxHash,
			"finalPrice": data.Price,
		})
	}
	return nil
}

func (s *EventService) handleOrderCancelled(event *domain.ContractEvent) error {
	var data domain.OrderCancelledData
	if err := json.Unmarshal(event.EventData, &data); err != nil {
		return fmt.Errorf("unmarshal OrderCancelled: %w", err)
	}

	order, _ := s.orderRepo.FindByHash(data.OrderHash)
	if order != nil {
		_ = s.orderRepo.UpdateStatus(data.OrderHash, domain.Cancelled)
		_ = s.cache.InvalidateOrders(context.Background(), order.Collection)
		s.broadcast(order.Collection, "order:cancelled", map[string]string{
			"maker": data.Maker,
		})
	}
	return nil
}

func (s *EventService) handleCounterIncremented(event *domain.ContractEvent) error {
	var data domain.CounterIncrementedData
	if err := json.Unmarshal(event.EventData, &data); err != nil {
		return fmt.Errorf("unmarshal CounterIncremented: %w", err)
	}

	minCounter := new(big.Int)
	minCounter.SetString(data.Counter, 10)

	if err := s.orderRepo.CancelByMakerCounter(data.Maker, minCounter); err != nil {
		return fmt.Errorf("cancel by counter: %w", err)
	}

	// CounterIncremented doesn't have a specific collection; broadcast
	// maker-level cancellation so subscribed clients update.
	makerPayload, _ := json.Marshal(map[string]string{"maker": data.Maker})
	s.hub.Broadcast("", ws.Message{Type: "order:cancelled", Payload: makerPayload})
	return nil
}

func (s *EventService) handleCollectionUpdated(event *domain.ContractEvent) error {
	var raw struct {
		Collection string `json:"collection"`
		Blocked    bool   `json:"blocked"`
	}
	if err := json.Unmarshal(event.EventData, &raw); err != nil {
		return fmt.Errorf("unmarshal CollectionUpdated: %w", err)
	}

	if raw.Blocked {
		if err := s.orderRepo.CancelByCollection(raw.Collection); err != nil {
			return fmt.Errorf("cancel by collection: %w", err)
		}
		_ = s.cache.InvalidateOrders(context.Background(), raw.Collection)
	}

	s.broadcast(raw.Collection, "collection:updated", map[string]interface{}{
		"collection": raw.Collection,
		"blocked":    raw.Blocked,
	})
	return nil
}

func (s *EventService) broadcast(collection string, eventType string, payload interface{}) {
	data, _ := json.Marshal(payload)
	s.hub.Broadcast(collection, ws.Message{Type: eventType, Payload: data})
}
