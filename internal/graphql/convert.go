package graphql

import (
	"fmt"
	"math/big"
	"time"

	"nft-market-backend/internal/domain"
)

func domainOrderToGQL(o *domain.Order) *Order {
	if o == nil {
		return nil
	}
	taker := o.Taker
	if taker == "0x0000000000000000000000000000000000000000" {
		taker = ""
	}
	hash := o.OrderHash
	createdAt := o.CreatedAt.Format(time.RFC3339)
	return &Order{
		Hash:         hash,
		Maker:        o.Maker,
		Taker:        strPtrOrNil(taker),
		Side:         int(o.Side),
		Kind:         int(o.Kind),
		AssetType:    int(o.AssetType),
		Collection:   o.Collection,
		TokenID:      new(big.Int).Set(o.TokenID.Int),
		Amount:       new(big.Int).Set(o.Amount.Int),
		Price:        new(big.Int).Set(o.Price.Int),
		StartPrice:   new(big.Int).Set(o.StartPrice.Int),
		StartTime:    fmt.Sprintf("%d", o.StartTime),
		EndTime:      fmt.Sprintf("%d", o.EndTime),
		Salt:         new(big.Int).Set(o.Salt.Int),
		Signature:    o.Signature,
		Status:       int(o.Status),
		Counter:      new(big.Int).Set(o.Counter.Int),
		CurrentPrice: o.CurrentPrice(time.Now()),
		Extra:        o.Extra,
		CreatedAt:    createdAt,
	}
}

func domainOrdersToGQL(orders []domain.Order) []*Order {
	result := make([]*Order, len(orders))
	for i := range orders {
		result[i] = domainOrderToGQL(&orders[i])
	}
	return result
}

func gqlFilterToDomain(f *OrderFilter) domain.OrderFilter {
	df := domain.OrderFilter{}
	if f.Collection != nil {
		df.Collection = *f.Collection
	}
	if f.Maker != nil {
		df.Maker = *f.Maker
	}
	if f.Side != nil {
		s := domain.OrderSide(*f.Side)
		df.Side = &s
	}
	if f.Kind != nil {
		k := domain.OrderKind(*f.Kind)
		df.Kind = &k
	}
	if f.AssetType != nil {
		a := domain.AssetType(*f.AssetType)
		df.AssetType = &a
	}
	if f.Status != nil {
		s := domain.OrderStatus(*f.Status)
		df.Status = &s
	}
	if f.MinPrice != nil {
		df.MinPrice = domain.NewBigInt(nil)
		df.MinPrice.Int.SetString(*f.MinPrice, 10)
	}
	if f.MaxPrice != nil {
		df.MaxPrice = domain.NewBigInt(nil)
		df.MaxPrice.Int.SetString(*f.MaxPrice, 10)
	}
	return df
}

func gqlSubmitToDomain(input SubmitOrderInput) domain.SubmitOrderRequest {
	req := domain.SubmitOrderRequest{
		Maker:      input.Maker,
		Side:       domain.OrderSide(input.Side),
		Kind:       domain.OrderKind(input.Kind),
		AssetType:  domain.AssetType(input.AssetType),
		Collection: input.Collection,
		TokenID:    input.TokenID,
		Amount:     input.Amount,
		Price:      input.Price,
		StartTime:  uint64(input.StartTime),
		EndTime:    uint64(input.EndTime),
		Salt:       input.Salt,
		Signature:  input.Signature,
	}
	if input.Taker != nil {
		req.Taker = *input.Taker
	}
	if input.PaymentToken != nil {
		req.PaymentToken = *input.PaymentToken
	}
	if input.StartPrice != nil {
		req.StartPrice = *input.StartPrice
	}
	if input.Extra != nil {
		req.Extra = *input.Extra
	}
	return req
}

func domainCollectionToGQL(c *domain.Collection, floorPrice, bestBid *big.Int, listedCount int64) *Collection {
	if c == nil {
		return nil
	}
	return &Collection{
		Address:     c.Address,
		Name:        c.Name,
		Symbol:      c.Symbol,
		Image:       c.ImageURL,
		FloorPrice:  floorPrice,
		BestBid:     bestBid,
		ListedCount: int(listedCount),
		CreatedAt:   c.SyncedAt.Format(time.RFC3339),
	}
}

func strPtrOrNil(s string) *string {
	if s == "" || s == "0x0000000000000000000000000000000000000000" {
		return nil
	}
	return &s
}
