package repository

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"nft-market-backend/internal/domain"
)

// CollectionRepo persists collection and NFT metadata to PostgreSQL.
type CollectionRepo struct {
	db      *sql.DB
	ChainID int64
}

// NewCollectionRepo creates a CollectionRepo with the given database connection.
func NewCollectionRepo(db *sql.DB) *CollectionRepo {
	return &CollectionRepo{db: db}
}

// Upsert inserts or updates a collection record.
func (r *CollectionRepo) Upsert(c *domain.Collection) error {
	addr, err := hexDecode(c.Address)
	if err != nil {
		return fmt.Errorf("decode address: %w", err)
	}

	_, err = r.db.ExecContext(context.Background(),
		`INSERT INTO collections (address, chain_id, name, symbol, image_url, metadata, synced_at)
		 VALUES ($1, $2, $3, $4, $5, $6, NOW())
		 ON CONFLICT (address) DO UPDATE SET
		     name = $3, symbol = $4, image_url = $5, metadata = $6, synced_at = NOW()`,
		addr, r.ChainID, c.Name, c.Symbol, c.ImageURL, c.Metadata,
	)
	if err != nil {
		return fmt.Errorf("upsert collection: %w", err)
	}
	return nil
}

// FindAll returns collections with optional search filter and pagination.
func (r *CollectionRepo) FindAll(search string, page, pageSize int) ([]domain.Collection, int64, error) {
	if pageSize <= 0 {
		pageSize = 20
	}
	if page <= 0 {
		page = 1
	}
	offset := (page - 1) * pageSize

	var total int64
	var args []interface{}

	whereClause := "1=1"
	if search != "" {
		whereClause = "(name ILIKE $1 OR symbol ILIKE $1)"
		args = append(args, "%"+search+"%")
	}

	// Count
	countQuery := "SELECT COUNT(*) FROM collections WHERE " + whereClause
	err := r.db.QueryRowContext(context.Background(), countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count collections: %w", err)
	}

	// Data
	paramIdx := len(args)
	dataQuery := fmt.Sprintf(
		"SELECT address, chain_id, name, symbol, image_url, metadata, synced_at FROM collections WHERE %s ORDER BY synced_at DESC LIMIT $%d OFFSET $%d",
		whereClause, paramIdx+1, paramIdx+2,
	)
	dataArgs := append(args, pageSize, offset)

	rows, err := r.db.QueryContext(context.Background(), dataQuery, dataArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("query collections: %w", err)
	}

	collections, err := scanCollections(rows)
	if err != nil {
		return nil, 0, err
	}
	return collections, total, nil
}

// FindByAddress returns a single collection by its contract address.
func (r *CollectionRepo) FindByAddress(addr string) (*domain.Collection, error) {
	addrBytes, err := hexDecode(addr)
	if err != nil {
		return nil, fmt.Errorf("decode address: %w", err)
	}

	query := `SELECT address, chain_id, name, symbol, image_url, metadata, synced_at
		  FROM collections WHERE address = $1`
	rows, err := r.db.QueryContext(context.Background(), query, addrBytes)
	if err != nil {
		return nil, fmt.Errorf("query collection by address: %w", err)
	}

	collections, err := scanCollections(rows)
	if err != nil {
		return nil, err
	}
	if len(collections) == 0 {
		return nil, nil
	}
	return &collections[0], nil
}

// UpsertNFTMetadata inserts or updates NFT metadata.
func (r *CollectionRepo) UpsertNFTMetadata(m *domain.NFTMetadata) error {
	coll, err := hexDecode(m.Collection)
	if err != nil {
		return fmt.Errorf("decode collection: %w", err)
	}

	_, err = r.db.ExecContext(context.Background(),
		`INSERT INTO nft_metadata (collection, token_id, name, description, image_url, attributes, synced_at)
		 VALUES ($1, $2, $3, $4, $5, $6, NOW())
		 ON CONFLICT (collection, token_id) DO UPDATE SET
		     name = $3, description = $4, image_url = $5, attributes = $6, synced_at = NOW()`,
		coll, bigIntStr(m.TokenID), m.Name, m.Description, m.ImageURL, m.Attributes,
	)
	if err != nil {
		return fmt.Errorf("upsert nft metadata: %w", err)
	}
	return nil
}

// GetNFTMetadata returns metadata for a specific NFT, or nil if not found.
func (r *CollectionRepo) GetNFTMetadata(collection string, tokenID *domain.BigInt) (*domain.NFTMetadata, error) {
	coll, err := hexDecode(collection)
	if err != nil {
		return nil, fmt.Errorf("decode collection: %w", err)
	}

	query := `SELECT collection, token_id, name, description, image_url, attributes, synced_at
		  FROM nft_metadata WHERE collection = $1 AND token_id = $2`
	rows, err := r.db.QueryContext(context.Background(), query, coll, bigIntStr(tokenID))
	if err != nil {
		return nil, fmt.Errorf("query nft metadata: %w", err)
	}

	metas, err := scanNFTMetadata(rows)
	if err != nil {
		return nil, err
	}
	if len(metas) == 0 {
		return nil, nil
	}
	return &metas[0], nil
}

// GetStaleMetadata returns NFT metadata records that haven't been synced in 24 hours.
func (r *CollectionRepo) GetStaleMetadata(limit int) ([]domain.NFTMetadata, error) {
	if limit <= 0 {
		limit = 100
	}

	query := `SELECT collection, token_id, name, description, image_url, attributes, synced_at
		  FROM nft_metadata WHERE synced_at < NOW() - INTERVAL '24 hours'
		  ORDER BY synced_at ASC LIMIT $1`
	rows, err := r.db.QueryContext(context.Background(), query, limit)
	if err != nil {
		return nil, fmt.Errorf("query stale metadata: %w", err)
	}
	return scanNFTMetadata(rows)
}

// GetStaleCollections returns collections that haven't been synced in 24 hours.
func (r *CollectionRepo) GetStaleCollections(limit int) ([]domain.Collection, error) {
	if limit <= 0 {
		limit = 100
	}

	query := `SELECT address, chain_id, name, symbol, image_url, metadata, synced_at
		  FROM collections WHERE synced_at < NOW() - INTERVAL '24 hours'
		  ORDER BY synced_at ASC LIMIT $1`
	rows, err := r.db.QueryContext(context.Background(), query, limit)
	if err != nil {
		return nil, fmt.Errorf("query stale collections: %w", err)
	}
	return scanCollections(rows)
}

// FindAllCollections returns all collections without pagination.
func (r *CollectionRepo) FindAllCollections() ([]domain.Collection, error) {
	query := `SELECT address, chain_id, name, symbol, image_url, metadata, synced_at
		  FROM collections ORDER BY synced_at DESC`
	rows, err := r.db.QueryContext(context.Background(), query)
	if err != nil {
		return nil, fmt.Errorf("query all collections: %w", err)
	}
	return scanCollections(rows)
}

// GetCollectionCount returns the total number of collections.
func (r *CollectionRepo) GetCollectionCount() (int64, error) {
	var count int64
	err := r.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM collections`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count collections: %w", err)
	}
	return count, nil
}

// GetTotalOrders returns the total number of orders (all statuses).
func (r *CollectionRepo) GetTotalOrders() (int64, error) {
	var count int64
	err := r.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM orders`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count orders: %w", err)
	}
	return count, nil
}

func scanCollections(rows *sql.Rows) ([]domain.Collection, error) {
	defer rows.Close()
	var collections []domain.Collection
	for rows.Next() {
		var c domain.Collection
		var addr []byte
		var metadata json.RawMessage
		err := rows.Scan(&addr, &c.ChainID, &c.Name, &c.Symbol, &c.ImageURL, &metadata, &c.SyncedAt)
		if err != nil {
			return nil, fmt.Errorf("scan collection: %w", err)
		}
		c.Address = "0x" + hex.EncodeToString(addr)
		c.Metadata = metadata
		collections = append(collections, c)
	}
	return collections, rows.Err()
}

func scanNFTMetadata(rows *sql.Rows) ([]domain.NFTMetadata, error) {
	defer rows.Close()
	var metas []domain.NFTMetadata
	for rows.Next() {
		var m domain.NFTMetadata
		var coll []byte
		var tokenIDStr string
		var attrs json.RawMessage
		err := rows.Scan(&coll, &tokenIDStr, &m.Name, &m.Description, &m.ImageURL, &attrs, &m.SyncedAt)
		if err != nil {
			return nil, fmt.Errorf("scan nft metadata: %w", err)
		}
		m.Collection = "0x" + hex.EncodeToString(coll)
		m.TokenID = scanBigInt(tokenIDStr)
		m.Attributes = attrs
		metas = append(metas, m)
	}
	return metas, rows.Err()
}
