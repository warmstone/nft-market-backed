package repository

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"

	"nft-market-backend/internal/domain"
)

// OrderRepo persists and queries orders in PostgreSQL.
// All addresses and hashes are stored as BYTEA and converted to "0x..." hex
// strings at the boundary. NUMERIC columns are converted via big.Int.String().
type OrderRepo struct {
	db      *sql.DB
	ChainID int64
}

// NewOrderRepo creates an OrderRepo with the given database connection.
func NewOrderRepo(db *sql.DB) *OrderRepo {
	return &OrderRepo{db: db}
}

// ---------- helpers ----------

func hexDecode(s string) ([]byte, error) {
	if s == "" {
		return nil, nil
	}
	return hex.DecodeString(strings.TrimPrefix(s, "0x"))
}

func hexEncode(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return "0x" + hex.EncodeToString(b)
}

func bigIntStr(bi *domain.BigInt) string {
	if bi == nil || bi.Int == nil {
		return "0"
	}
	return bi.Int.String()
}

func scanBigInt(s string) *domain.BigInt {
	i := new(big.Int)
	if s != "" {
		i.SetString(s, 10)
	}
	return &domain.BigInt{Int: i}
}

func rawBigInt(s string) *big.Int {
	i := new(big.Int)
	if s != "" {
		i.SetString(s, 10)
	}
	return i
}

func splitSignature(sig string) (r, s []byte, v uint8, err error) {
	raw := strings.TrimPrefix(sig, "0x")
	if len(raw) != 130 {
		return nil, nil, 0, fmt.Errorf("invalid signature length: expected 130 hex chars, got %d", len(raw))
	}
	r, err = hex.DecodeString(raw[0:64])
	if err != nil {
		return nil, nil, 0, fmt.Errorf("decode signature r: %w", err)
	}
	s, err = hex.DecodeString(raw[64:128])
	if err != nil {
		return nil, nil, 0, fmt.Errorf("decode signature s: %w", err)
	}
	vByte, err := hex.DecodeString(raw[128:130])
	if err != nil {
		return nil, nil, 0, fmt.Errorf("decode signature v: %w", err)
	}
	return r, s, vByte[0], nil
}

func joinSignature(r, s []byte, v uint8) string {
	return "0x" + hex.EncodeToString(r) + hex.EncodeToString(s) + fmt.Sprintf("%02x", v)
}

// scanOrders reads all rows from the standard orders SELECT query into domain.Order values.
func scanOrders(rows *sql.Rows) ([]domain.Order, error) {
	defer rows.Close()
	var orders []domain.Order
	for rows.Next() {
		var o domain.Order
		var orderHash, maker, taker, collection, paymentToken, extra []byte
		var sigR, sigS []byte
		var tokenIDStr, amountStr, priceStr, startPriceStr, saltStr, counterStr string
		var startTime, endTime int64
		var sigV uint8
		var createdAt, updatedAt sql.NullTime
		var expiredAt sql.NullTime
		var chainID int64

		err := rows.Scan(
			&o.ID, &orderHash, &chainID, &maker, &taker,
			&o.Side, &o.Kind, &o.AssetType, &collection,
			&tokenIDStr, &amountStr, &paymentToken, &priceStr, &startPriceStr,
			&startTime, &endTime, &saltStr, &counterStr, &extra,
			&sigR, &sigS, &sigV, &o.Status,
			&createdAt, &updatedAt, &expiredAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan order: %w", err)
		}

		o.OrderHash = hexEncode(orderHash)
		o.Maker = hexEncode(maker)
		o.Taker = hexEncode(taker)
		o.Collection = hexEncode(collection)
		o.TokenID = scanBigInt(tokenIDStr)
		o.Amount = scanBigInt(amountStr)
		o.PaymentToken = hexEncode(paymentToken)
		o.Price = scanBigInt(priceStr)
		o.StartPrice = scanBigInt(startPriceStr)
		o.StartTime = uint64(startTime)
		o.EndTime = uint64(endTime)
		o.Salt = scanBigInt(saltStr)
		o.Counter = scanBigInt(counterStr)
		o.Extra = hexEncode(extra)
		o.Signature = joinSignature(sigR, sigS, sigV)
		if createdAt.Valid {
			o.CreatedAt = createdAt.Time
		}
		if updatedAt.Valid {
			o.UpdatedAt = updatedAt.Time
		}

		orders = append(orders, o)
	}
	return orders, rows.Err()
}

const ordersSelectColumns = `id, order_hash, chain_id, maker, taker, side, kind, asset_type,
collection, token_id, amount, payment_token, price, start_price, start_time, end_time,
salt, counter, extra, signature_r, signature_s, signature_v, status,
created_at, updated_at, expired_at`

// ---------- public methods ----------

// Insert stores a new order. It decodes hex strings to BYTEA, NUMERIC to
// decimal strings, and splits the signature into r,s,v components.
func (r *OrderRepo) Insert(o *domain.Order) error {
	hashB, err := hexDecode(o.OrderHash)
	if err != nil {
		return fmt.Errorf("decode order_hash: %w", err)
	}
	makerB, err := hexDecode(o.Maker)
	if err != nil {
		return fmt.Errorf("decode maker: %w", err)
	}
	takerB, err := hexDecode(o.Taker)
	if err != nil {
		return fmt.Errorf("decode taker: %w", err)
	}
	collectionB, err := hexDecode(o.Collection)
	if err != nil {
		return fmt.Errorf("decode collection: %w", err)
	}
	paymentB, err := hexDecode(o.PaymentToken)
	if err != nil {
		return fmt.Errorf("decode payment_token: %w", err)
	}
	extraB, err := hexDecode(o.Extra)
	if err != nil {
		return fmt.Errorf("decode extra: %w", err)
	}
	sigR, sigS, sigV, err := splitSignature(o.Signature)
	if err != nil {
		return fmt.Errorf("split signature: %w", err)
	}

	var expiredAt interface{}
	if o.EndTime > 0 {
		expiredAt = sql.NullTime{Time: unixTime(o.EndTime), Valid: true}
	}

	query := `INSERT INTO orders (
		order_hash, chain_id, maker, taker, side, kind, asset_type, collection,
		token_id, amount, payment_token, price, start_price, start_time, end_time,
		salt, counter, extra, signature_r, signature_s, signature_v, status, expired_at
	) VALUES (
		$1, $2, $3, $4, $5, $6, $7, $8,
		$9, $10, $11, $12, $13, $14, $15,
		$16, $17, $18, $19, $20, $21, $22, $23
	)`

	_, err = r.db.ExecContext(context.Background(), query,
		hashB, r.ChainID, makerB, takerB,
		o.Side, o.Kind, o.AssetType, collectionB,
		bigIntStr(o.TokenID), bigIntStr(o.Amount), paymentB, bigIntStr(o.Price), bigIntStr(o.StartPrice),
		int64(o.StartTime), int64(o.EndTime),
		bigIntStr(o.Salt), bigIntStr(o.Counter), extraB,
		sigR, sigS, sigV, o.Status,
		expiredAt,
	)
	if err != nil {
		return fmt.Errorf("insert order: %w", err)
	}
	return nil
}

// Find queries orders with dynamic filters. Returns matching orders and total count.
func (r *OrderRepo) Find(filter domain.OrderFilter) ([]domain.Order, int64, error) {
	var conditions []string
	var args []interface{}

	if filter.Collection != "" {
		b, err := hexDecode(filter.Collection)
		if err != nil {
			return nil, 0, fmt.Errorf("decode collection: %w", err)
		}
		args = append(args, b)
		conditions = append(conditions, fmt.Sprintf("collection = $%d", len(args)))
	}
	if filter.Side != nil {
		args = append(args, *filter.Side)
		conditions = append(conditions, fmt.Sprintf("side = $%d", len(args)))
	}
	if filter.Kind != nil {
		args = append(args, *filter.Kind)
		conditions = append(conditions, fmt.Sprintf("kind = $%d", len(args)))
	}
	if filter.AssetType != nil {
		args = append(args, *filter.AssetType)
		conditions = append(conditions, fmt.Sprintf("asset_type = $%d", len(args)))
	}
	if filter.Maker != "" {
		b, err := hexDecode(filter.Maker)
		if err != nil {
			return nil, 0, fmt.Errorf("decode maker: %w", err)
		}
		args = append(args, b)
		conditions = append(conditions, fmt.Sprintf("maker = $%d", len(args)))
	}
	if filter.TokenID != nil && filter.TokenID.Int != nil {
		args = append(args, bigIntStr(filter.TokenID))
		conditions = append(conditions, fmt.Sprintf("token_id = $%d", len(args)))
	}
	if filter.PaymentToken != "" {
		b, err := hexDecode(filter.PaymentToken)
		if err != nil {
			return nil, 0, fmt.Errorf("decode payment_token: %w", err)
		}
		args = append(args, b)
		conditions = append(conditions, fmt.Sprintf("payment_token = $%d", len(args)))
	}
	if filter.Status != nil {
		args = append(args, *filter.Status)
		conditions = append(conditions, fmt.Sprintf("status = $%d", len(args)))
	}
	if filter.MinPrice != nil && filter.MinPrice.Int != nil {
		args = append(args, filter.MinPrice.Int.String())
		conditions = append(conditions, fmt.Sprintf("price >= $%d", len(args)))
	}
	if filter.MaxPrice != nil && filter.MaxPrice.Int != nil {
		args = append(args, filter.MaxPrice.Int.String())
		conditions = append(conditions, fmt.Sprintf("price <= $%d", len(args)))
	}

	whereClause := "1=1"
	if len(conditions) > 0 {
		whereClause = strings.Join(conditions, " AND ")
	}

	// Count total matching rows
	var total int64
	countQuery := "SELECT COUNT(*) FROM orders WHERE " + whereClause
	err := r.db.QueryRowContext(context.Background(), countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count orders: %w", err)
	}

	// Default limit / offset
	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	// Build sort
	orderBy := "created_at DESC"

	dataQuery := fmt.Sprintf(
		"SELECT %s FROM orders WHERE %s ORDER BY %s LIMIT $%d OFFSET $%d",
		ordersSelectColumns, whereClause, orderBy, len(args)+1, len(args)+2,
	)
	dataArgs := append(args, limit, offset)

	rows, err := r.db.QueryContext(context.Background(), dataQuery, dataArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("query orders: %w", err)
	}

	orders, err := scanOrders(rows)
	if err != nil {
		return nil, 0, err
	}
	return orders, total, nil
}

// FindByHash returns a single order by its hash, or nil if not found.
func (r *OrderRepo) FindByHash(hash string) (*domain.Order, error) {
	b, err := hexDecode(hash)
	if err != nil {
		return nil, fmt.Errorf("decode hash: %w", err)
	}

	query := "SELECT " + ordersSelectColumns + " FROM orders WHERE order_hash = $1"
	rows, err := r.db.QueryContext(context.Background(), query, b)
	if err != nil {
		return nil, fmt.Errorf("query order by hash: %w", err)
	}

	orders, err := scanOrders(rows)
	if err != nil {
		return nil, err
	}
	if len(orders) == 0 {
		return nil, nil
	}
	return &orders[0], nil
}

// UpdateStatus sets the order status. Only transitions from Active (0) are
// permitted to prevent race conditions.
func (r *OrderRepo) UpdateStatus(hash string, status domain.OrderStatus) error {
	b, err := hexDecode(hash)
	if err != nil {
		return fmt.Errorf("decode hash: %w", err)
	}

	query := `UPDATE orders SET status = $1, updated_at = NOW() WHERE order_hash = $2 AND status = 0`
	_, err = r.db.ExecContext(context.Background(), query, status, b)
	if err != nil {
		return fmt.Errorf("update order status: %w", err)
	}
	return nil
}

// CancelByMakerSalt cancels active orders matching the given maker and salt.
func (r *OrderRepo) CancelByMakerSalt(maker string, salt *big.Int) error {
	makerB, err := hexDecode(maker)
	if err != nil {
		return fmt.Errorf("decode maker: %w", err)
	}

	query := `UPDATE orders SET status = 2, updated_at = NOW() WHERE maker = $1 AND salt = $2 AND status = 0`
	_, err = r.db.ExecContext(context.Background(), query, makerB, salt.String())
	if err != nil {
		return fmt.Errorf("cancel by maker+salt: %w", err)
	}
	return nil
}

// CancelByMakerCounter cancels active orders from a maker whose counter is
// below the given minimum (used when the maker increments their on-chain counter).
func (r *OrderRepo) CancelByMakerCounter(maker string, minCounter *big.Int) error {
	makerB, err := hexDecode(maker)
	if err != nil {
		return fmt.Errorf("decode maker: %w", err)
	}

	query := `UPDATE orders SET status = 2, updated_at = NOW() WHERE maker = $1 AND counter < $2 AND status = 0`
	_, err = r.db.ExecContext(context.Background(), query, makerB, minCounter.String())
	if err != nil {
		return fmt.Errorf("cancel by maker+counter: %w", err)
	}
	return nil
}

// ExpireOrders marks all active orders whose expired_at timestamp has passed
// as status Expired. Returns the number of rows affected.
func (r *OrderRepo) ExpireOrders() (int64, error) {
	query := `UPDATE orders SET status = 3, updated_at = NOW() WHERE status = 0 AND expired_at IS NOT NULL AND expired_at <= NOW()`
	result, err := r.db.ExecContext(context.Background(), query)
	if err != nil {
		return 0, fmt.Errorf("expire orders: %w", err)
	}
	n, _ := result.RowsAffected()
	return n, nil
}

// CancelByCollection cancels all active orders for a given collection.
func (r *OrderRepo) CancelByCollection(collection string) error {
	collB, err := hexDecode(collection)
	if err != nil {
		return fmt.Errorf("decode collection: %w", err)
	}

	query := `UPDATE orders SET status = 2, updated_at = NOW() WHERE collection = $1 AND status = 0`
	_, err = r.db.ExecContext(context.Background(), query, collB)
	if err != nil {
		return fmt.Errorf("cancel by collection: %w", err)
	}
	return nil
}

// GetActiveMakerCount returns the number of distinct makers with at least one
// active order.
func (r *OrderRepo) GetActiveMakerCount() (int64, error) {
	var count int64
	query := `SELECT COUNT(DISTINCT maker) FROM orders WHERE status = 0`
	err := r.db.QueryRowContext(context.Background(), query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count active makers: %w", err)
	}
	return count, nil
}

// GetBestPrice returns the best (lowest for Sell, highest for Buy) price among
// active orders for a collection and side.
func (r *OrderRepo) GetBestPrice(collection string, side domain.OrderSide) (*big.Int, error) {
	collB, err := hexDecode(collection)
	if err != nil {
		return nil, fmt.Errorf("decode collection: %w", err)
	}

	dir := "ASC"
	if side == domain.Buy {
		dir = "DESC"
	}

	query := fmt.Sprintf(
		"SELECT price FROM orders WHERE collection = $1 AND side = $2 AND status = 0 ORDER BY price %s LIMIT 1",
		dir,
	)
	var priceStr string
	err = r.db.QueryRowContext(context.Background(), query, collB, side).Scan(&priceStr)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get best price: %w", err)
	}
	return rawBigInt(priceStr), nil
}

// GetListedCount returns the number of active Sell orders for a collection.
func (r *OrderRepo) GetListedCount(collection string) (int64, error) {
	collB, err := hexDecode(collection)
	if err != nil {
		return 0, fmt.Errorf("decode collection: %w", err)
	}

	var count int64
	query := `SELECT COUNT(*) FROM orders WHERE collection = $1 AND side = 0 AND status = 0`
	err = r.db.QueryRowContext(context.Background(), query, collB).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count listed: %w", err)
	}
	return count, nil
}

// GetMakerActiveCollections returns distinct collection addresses where the
// maker has at least one active order.
func (r *OrderRepo) GetMakerActiveCollections(maker string) ([]string, error) {
	makerB, err := hexDecode(maker)
	if err != nil {
		return nil, fmt.Errorf("decode maker: %w", err)
	}

	rows, err := r.db.QueryContext(context.Background(),
		`SELECT DISTINCT collection FROM orders WHERE maker = $1 AND status = 0`, makerB)
	if err != nil {
		return nil, fmt.Errorf("query maker collections: %w", err)
	}
	defer rows.Close()

	var collections []string
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			return nil, fmt.Errorf("scan collection: %w", err)
		}
		collections = append(collections, hexEncode(b))
	}
	return collections, rows.Err()
}

// unixTime converts a uint64 unix timestamp to time.Time in UTC.
func unixTime(ts uint64) time.Time {
	return time.Unix(int64(ts), 0).UTC()
}
