package mpesa

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/kyambuthia/kikapu/internal/payment"
)

// PostgresCallbackStore persists processed M-Pesa STK callbacks in PostgreSQL
// so duplicate callback suppression survives server restarts.
type PostgresCallbackStore struct {
	db *sql.DB
}

func NewPostgresCallbackStore(db *sql.DB) *PostgresCallbackStore {
	return &PostgresCallbackStore{db: db}
}

func (s *PostgresCallbackStore) GetCallback(checkoutRequestID string, resultCode int) (*ProcessedCallback, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var (
		cb    ProcessedCallback
		state string
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT checkout_request_id, merchant_request_id, result_code, result_desc,
		       amount, mpesa_receipt, phone_number, transaction_date, state
		FROM mpesa_callbacks
		WHERE checkout_request_id = $1 AND result_code = $2
	`, checkoutRequestID, resultCode).Scan(
		&cb.CheckoutRequestID,
		&cb.MerchantRequestID,
		&cb.ResultCode,
		&cb.ResultDesc,
		&cb.Amount,
		&cb.MpesaReceipt,
		&cb.PhoneNumber,
		&cb.TransactionDate,
		&state,
	)
	if errors.Is(err, sql.ErrNoRows) || err != nil {
		return nil, false
	}
	cb.State = payment.State(state)
	return &cb, true
}

func (s *PostgresCallbackStore) SaveCallback(cb *ProcessedCallback) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO mpesa_callbacks (
			checkout_request_id, result_code, merchant_request_id, result_desc,
			amount, mpesa_receipt, phone_number, transaction_date, state
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (checkout_request_id, result_code) DO NOTHING
	`, cb.CheckoutRequestID, cb.ResultCode, cb.MerchantRequestID, cb.ResultDesc,
		cb.Amount, cb.MpesaReceipt, cb.PhoneNumber, cb.TransactionDate, string(cb.State))
	return err
}
