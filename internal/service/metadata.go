package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"nft-market-backend/internal/domain"
	"nft-market-backend/internal/repository"

	"github.com/ethereum/go-ethereum/common"
)

type fetchJob struct {
	Collection string
	TokenID    *domain.BigInt
}

// MetadataService fetches off-chain NFT metadata from tokenURI / IPFS.
type MetadataService struct {
	collectionRepo *repository.CollectionRepo
	rpcCall        func(ctx context.Context, to common.Address, data []byte) ([]byte, error)
	httpClient     *http.Client
	ipfsGateway    string
	queue          chan fetchJob
}

// NewMetadataService creates a MetadataService.
func NewMetadataService(
	collectionRepo *repository.CollectionRepo,
	rpcCall func(ctx context.Context, to common.Address, data []byte) ([]byte, error),
	ipfsGateway string,
) *MetadataService {
	return &MetadataService{
		collectionRepo: collectionRepo,
		rpcCall:        rpcCall,
		httpClient:     &http.Client{Timeout: 5 * time.Second},
		ipfsGateway:    ipfsGateway,
		queue:          make(chan fetchJob, 1000),
	}
}

// Enqueue adds a metadata fetch job to the queue. Non-blocking; drops if full.
func (s *MetadataService) Enqueue(collection string, tokenID string) {
	tid := domain.NewBigInt(nil)
	tid.Int.SetString(tokenID, 10)

	select {
	case s.queue <- fetchJob{Collection: collection, TokenID: tid}:
	default:
	}
}

// Run starts the metadata fetcher workers.
func (s *MetadataService) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case job := <-s.queue:
					s.fetchWithRetry(ctx, job)
				}
			}
		}()
	}
	wg.Wait()
}

func (s *MetadataService) fetchWithRetry(ctx context.Context, job fetchJob) {
	for attempt := 0; attempt < 3; attempt++ {
		if err := s.fetchOne(ctx, job.Collection, job.TokenID); err == nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(1<<attempt) * time.Second):
		}
	}
}

func (s *MetadataService) fetchOne(ctx context.Context, collection string, tokenID *domain.BigInt) error {
	// Call tokenURI(uint256).
	data := append([]byte{}, common.Hex2Bytes("c87b56dd")...) // tokenURI selector
	data = append(data, common.LeftPadBytes(tokenID.Int.Bytes(), 32)...)

	result, err := s.rpcCall(ctx, common.HexToAddress(collection), data)
	if err != nil {
		return fmt.Errorf("tokenURI rpc call: %w", err)
	}

	uri := s.decodeStringResult(result)
	if uri == "" {
		return fmt.Errorf("empty tokenURI")
	}

	// Resolve IPFS.
	uri = s.resolveURI(uri)

	// Fetch JSON.
	req, err := http.NewRequestWithContext(ctx, "GET", uri, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch metadata: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
	if err != nil {
		return fmt.Errorf("read metadata body: %w", err)
	}

	var meta struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Image       string          `json:"image"`
		Attributes  json.RawMessage `json:"attributes"`
	}
	if err := json.Unmarshal(body, &meta); err != nil {
		// Non-JSON metadata is ok; store what we have.
		meta.Name = ""
	}

	nftMeta := &domain.NFTMetadata{
		Collection:  collection,
		TokenID:     tokenID,
		Name:        meta.Name,
		Description: meta.Description,
		ImageURL:    s.resolveURI(meta.Image),
		Attributes:  meta.Attributes,
	}
	return s.collectionRepo.UpsertNFTMetadata(nftMeta)
}

// FetchCollection fetches on-chain name and symbol for a collection.
func (s *MetadataService) FetchCollection(ctx context.Context, address string) error {
	addr := common.HexToAddress(address)

	name, _ := s.callString(ctx, addr, "0x06fdde03") // name()
	symbol, _ := s.callString(ctx, addr, "0x95d89b41") // symbol()

	c := &domain.Collection{
		Address:  address,
		ChainID:  0, // filled by caller/repo
		Name:     name,
		Symbol:   symbol,
		ImageURL: "",
	}
	return s.collectionRepo.Upsert(c)
}

// RefreshStale re-fetches metadata for all stale collections and NFTs.
func (s *MetadataService) RefreshStale(ctx context.Context) error {
	collections, err := s.collectionRepo.GetStaleCollections(100)
	if err != nil {
		return err
	}
	for _, c := range collections {
		_ = s.FetchCollection(ctx, c.Address)
	}

	nfts, err := s.collectionRepo.GetStaleMetadata(100)
	if err != nil {
		return err
	}
	for _, n := range nfts {
		s.Enqueue(n.Collection, n.TokenID.Int.String())
	}
	return nil
}

func (s *MetadataService) callString(ctx context.Context, to common.Address, selector string) (string, error) {
	result, err := s.rpcCall(ctx, to, common.Hex2Bytes(selector))
	if err != nil {
		return "", err
	}
	return s.decodeStringResult(result), nil
}

func (s *MetadataService) decodeStringResult(data []byte) string {
	if len(data) < 64 {
		return ""
	}
	// ABI-encoded: first 32 bytes = offset, next 32 bytes = length, then data.
	offset := domain.NewBigInt(nil)
	offset.Int.SetBytes(data[0:32])
	if offset.Int.Int64() != 32 {
		return ""
	}
	length := domain.NewBigInt(nil)
	length.Int.SetBytes(data[32:64])
	ln := length.Int.Int64()
	if ln <= 0 || int(64+ln) > len(data) {
		return ""
	}
	return string(data[64 : 64+ln])
}

func (s *MetadataService) resolveURI(uri string) string {
	if strings.HasPrefix(uri, "ipfs://") {
		cid := strings.TrimPrefix(uri, "ipfs://")
		return s.ipfsGateway + "/ipfs/" + cid
	}
	return uri
}

