package repository

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"nft-market-backend/internal/domain"
)

// EventRepo persists chain events to PostgreSQL.
type EventRepo struct {
	db *sql.DB
}

// NewEventRepo creates an EventRepo with the given database connection.
func NewEventRepo(db *sql.DB) *EventRepo {
	return &EventRepo{db: db}
}

// InsertEvent inserts a contract event. Uses ON CONFLICT DO NOTHING for
// idempotent replay — if the (block_number, tx_index, log_index) tuple already
// exists, the insert is silently skipped.
func (r *EventRepo) InsertEvent(e *domain.ContractEvent) error {
	txHash, err := hexDecode(e.TxHash)
	if err != nil {
		return fmt.Errorf("decode tx_hash: %w", err)
	}

	_, err = r.db.ExecContext(context.Background(),
		`INSERT INTO events (block_number, tx_hash, tx_index, log_index, event_name, event_data, removed)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (block_number, tx_index, log_index) DO NOTHING`,
		e.BlockNumber, txHash, e.TxIndex, e.LogIndex, e.EventName, e.EventData, e.Removed,
	)
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}
	return nil
}

// EventExists checks whether an event has already been recorded.
func (r *EventRepo) EventExists(blockNumber uint64, txIndex, logIndex uint) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM events WHERE block_number=$1 AND tx_index=$2 AND log_index=$3)`,
		blockNumber, txIndex, logIndex,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check event exists: %w", err)
	}
	return exists, nil
}

// MarkRemoved sets the removed flag on an event (reorg handling).
func (r *EventRepo) MarkRemoved(blockNumber uint64, txIndex, logIndex uint) error {
	_, err := r.db.ExecContext(context.Background(),
		`UPDATE events SET removed = true WHERE block_number = $1 AND tx_index = $2 AND log_index = $3`,
		blockNumber, txIndex, logIndex,
	)
	if err != nil {
		return fmt.Errorf("mark event removed: %w", err)
	}
	return nil
}

// GetLastSyncedBlock returns the highest synced block for the given chain.
// Returns 0 if no cursor exists yet.
func (r *EventRepo) GetLastSyncedBlock(chainID int64) (uint64, error) {
	var block uint64
	err := r.db.QueryRowContext(context.Background(),
		`SELECT last_synced_block FROM sync_cursor WHERE chain_id = $1`, chainID,
	).Scan(&block)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, fmt.Errorf("get last synced block: %w", err)
	}
	return block, nil
}

// UpdateLastSyncedBlock upserts the sync cursor for a chain.
func (r *EventRepo) UpdateLastSyncedBlock(chainID int64, block uint64) error {
	_, err := r.db.ExecContext(context.Background(),
		`INSERT INTO sync_cursor (chain_id, last_synced_block, updated_at)
		 VALUES ($1, $2, NOW())
		 ON CONFLICT (chain_id) DO UPDATE SET last_synced_block = $2, updated_at = NOW()`,
		chainID, block,
	)
	if err != nil {
		return fmt.Errorf("update last synced block: %w", err)
	}
	return nil
}

// GetUserActivity returns recent events involving a given address (as maker or
// taker in OrderFulfilled events).
func (r *EventRepo) GetUserActivity(address string, limit int) ([]domain.ContractEvent, error) {
	if limit <= 0 {
		limit = 20
	}

	addrBytes, err := hexDecode(address)
	if err != nil {
		return nil, fmt.Errorf("decode address: %w", err)
	}
	_ = addrBytes

	query := `SELECT id, block_number, tx_hash, tx_index, log_index, event_name, event_data, removed
		 FROM events
		 WHERE event_name = 'OrderFulfilled'
		   AND (event_data->>'maker' = $1 OR event_data->>'taker' = $1)
		 ORDER BY block_number DESC, tx_index DESC, log_index DESC
		 LIMIT $2`

	rows, err := r.db.QueryContext(context.Background(), query, strings.ToLower(address), limit)
	if err != nil {
		return nil, fmt.Errorf("get user activity: %w", err)
	}

	return scanEvents(rows)
}

// DeleteEventsAboveBlock removes events at or above a given block (used for
// reorg recovery — roll back and re-sync).
func (r *EventRepo) DeleteEventsAboveBlock(block uint64) error {
	_, err := r.db.ExecContext(context.Background(),
		`DELETE FROM events WHERE block_number >= $1`, block,
	)
	if err != nil {
		return fmt.Errorf("delete events above block: %w", err)
	}
	return nil
}

func scanEvents(rows *sql.Rows) ([]domain.ContractEvent, error) {
	defer rows.Close()
	var events []domain.ContractEvent
	for rows.Next() {
		var e domain.ContractEvent
		var txHash []byte
		var eventData json.RawMessage
		var id int64
		err := rows.Scan(&id, &e.BlockNumber, &txHash, &e.TxIndex, &e.LogIndex,
			&e.EventName, &eventData, &e.Removed)
		if err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		e.ID = id
		e.TxHash = "0x" + hex.EncodeToString(txHash)
		e.EventData = eventData
		events = append(events, e)
	}
	return events, rows.Err()
}
