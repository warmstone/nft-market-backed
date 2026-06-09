package rpc

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Client wraps an go-ethereum ethclient.Client, adding chain ID tracking
// and a set of contract addresses used as log filters for event queries.
type Client struct {
	eth     *ethclient.Client
	chainID int64
	addrs   []common.Address
}

// NewClient dials the given HTTP RPC endpoint and stores the chain ID.
func NewClient(httpURL string, chainID int64) (*Client, error) {
	eth, err := ethclient.Dial(httpURL)
	if err != nil {
		return nil, err
	}
	return &Client{
		eth:     eth,
		chainID: chainID,
	}, nil
}

// SetContractAddresses records the contract addresses to use as address
// filters in FilterLogs and SubscribeLogs queries.
func (c *Client) SetContractAddresses(exchange, protocolManager, collectionManager, royaltyManager common.Address) {
	c.addrs = []common.Address{exchange, protocolManager, collectionManager, royaltyManager}
}

// BlockNumber returns the most recent block number.
func (c *Client) BlockNumber(ctx context.Context) (uint64, error) {
	return c.eth.BlockNumber(ctx)
}

// FilterLogs executes an eth_getLogs query for the registered contract
// addresses over the given block range. All topics are included.
func (c *Client) FilterLogs(ctx context.Context, fromBlock, toBlock uint64) ([]types.Log, error) {
	query := ethereum.FilterQuery{
		FromBlock: new(big.Int).SetUint64(fromBlock),
		ToBlock:   new(big.Int).SetUint64(toBlock),
		Addresses: c.addrs,
	}
	return c.eth.FilterLogs(ctx, query)
}

// FilterLogsQuery executes an eth_getLogs query with a custom filter query.
func (c *Client) FilterLogsQuery(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
	if len(q.Addresses) == 0 {
		q.Addresses = c.addrs
	}
	return c.eth.FilterLogs(ctx, q)
}

// SubscribeLogs creates an eth_subscribe subscription for logs emitted by
// the registered contract addresses. Logs are delivered on ch.
func (c *Client) SubscribeLogs(ctx context.Context, ch chan<- types.Log) (ethereum.Subscription, error) {
	query := ethereum.FilterQuery{
		Addresses: c.addrs,
	}
	return c.eth.SubscribeFilterLogs(ctx, query, ch)
}

// CallContract performs an eth_call (read-only) against the given contract
// address with the supplied calldata. It is intended for tokenURI, name,
// symbol, and similar view functions.
func (c *Client) CallContract(ctx context.Context, to common.Address, data []byte) ([]byte, error) {
	msg := ethereum.CallMsg{
		To:   &to,
		Data: data,
	}
	return c.eth.CallContract(ctx, msg, nil)
}

// ChainID returns the chain ID of the connected network by querying the RPC.
func (c *Client) ChainID(ctx context.Context) (*big.Int, error) {
	return c.eth.ChainID(ctx)
}

// Close terminates the underlying RPC connection.
func (c *Client) Close() {
	c.eth.Close()
}
