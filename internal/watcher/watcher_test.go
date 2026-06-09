package watcher

import (
	"math/big"
	"testing"

	"nft-market-backend/internal/domain"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func topicAddress(addr string) common.Hash {
	return common.BytesToHash(common.HexToAddress(addr).Bytes())
}

func TestParseOrderFulfilledCurrentExchangeEvent(t *testing.T) {
	orderHash := common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	maker := "0x1111111111111111111111111111111111111111"
	taker := common.HexToAddress("0x2222222222222222222222222222222222222222")
	seller := common.HexToAddress("0x3333333333333333333333333333333333333333")
	buyer := common.HexToAddress("0x4444444444444444444444444444444444444444")
	collection := common.HexToAddress("0x5555555555555555555555555555555555555555")
	paymentToken := common.HexToAddress("0x0000000000000000000000000000000000000000")

	data, err := orderFulfilledABIArgs().Pack(
		taker,
		seller,
		buyer,
		uint8(0),
		uint8(0),
		collection,
		big.NewInt(42),
		big.NewInt(1),
		paymentToken,
		big.NewInt(1000),
		big.NewInt(25),
		big.NewInt(50),
	)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}

	event, err := (&Watcher{}).parseEvent(types.Log{
		Topics: []common.Hash{
			common.HexToHash(topicOrderFulfilled),
			orderHash,
			common.BigToHash(big.NewInt(123)),
			topicAddress(maker),
		},
		Data: data,
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if event == nil || event.EventName != domain.EventOrderFulfilled {
		t.Fatalf("unexpected event: %#v", event)
	}

	parsed, err := parseOrderFulfilled(types.Log{
		Topics: []common.Hash{
			common.HexToHash(topicOrderFulfilled),
			orderHash,
			common.BigToHash(big.NewInt(123)),
			topicAddress(maker),
		},
		Data: data,
	})
	if err != nil {
		t.Fatalf("parse fulfilled: %v", err)
	}
	if parsed.OrderHash != orderHash.Hex() || parsed.Salt != "123" || parsed.Maker != common.HexToAddress(maker).Hex() {
		t.Fatalf("indexed mismatch: %#v", parsed)
	}
	if parsed.Taker != taker.Hex() || parsed.Collection != collection.Hex() || parsed.Price != "1000" {
		t.Fatalf("data mismatch: %#v", parsed)
	}
}

func TestParseOrderCancelledCurrentExchangeEvent(t *testing.T) {
	maker := "0x1111111111111111111111111111111111111111"
	parsed, err := parseOrderCancelled(types.Log{
		Topics: []common.Hash{
			common.HexToHash(topicOrderCancelled),
			topicAddress(maker),
			common.BigToHash(big.NewInt(987)),
		},
	})
	if err != nil {
		t.Fatalf("parse cancelled: %v", err)
	}
	if parsed.Maker != common.HexToAddress(maker).Hex() || parsed.Salt != "987" {
		t.Fatalf("cancelled mismatch: %#v", parsed)
	}
}

func TestParseCollectionUpdatedCurrentEvent(t *testing.T) {
	boolT, _ := abi.NewType("bool", "", nil)
	data, err := abi.Arguments{{Type: boolT}, {Type: boolT}}.Pack(true, false)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}

	collection := "0x5555555555555555555555555555555555555555"
	parsed, err := parseCollectionUpdated(types.Log{
		Topics: []common.Hash{
			common.HexToHash(topicCollectionUpdated),
			topicAddress(collection),
		},
		Data: data,
	})
	if err != nil {
		t.Fatalf("parse collection: %v", err)
	}
	if parsed.Collection != common.HexToAddress(collection).Hex() || !parsed.Allowed || parsed.Blocked {
		t.Fatalf("collection mismatch: %#v", parsed)
	}
}
