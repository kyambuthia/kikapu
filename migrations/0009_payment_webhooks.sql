-- Persist Paystack webhook events and M-Pesa STK callbacks for restart-safe deduplication
-- Why: In-memory webhook stores lose idempotency state across process restarts
-- Reversible: yes

CREATE TABLE IF NOT EXISTS paystack_webhook_events (
    event_id TEXT PRIMARY KEY,
    reference TEXT NOT NULL,
    state TEXT NOT NULL,
    event_ts BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_paystack_webhook_events_reference ON paystack_webhook_events(reference);

CREATE TABLE IF NOT EXISTS mpesa_callbacks (
    checkout_request_id TEXT NOT NULL,
    result_code INT NOT NULL,
    merchant_request_id TEXT NOT NULL DEFAULT ,
    result_desc TEXT NOT NULL DEFAULT ,
    amount BIGINT NOT NULL DEFAULT 0,
    mpesa_receipt TEXT NOT NULL DEFAULT ,
    phone_number TEXT NOT NULL DEFAULT ,
    transaction_date BIGINT NOT NULL DEFAULT 0,
    state TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (checkout_request_id, result_code)
);

INSERT INTO schema_migrations (version) VALUES (0009) ON CONFLICT (version) DO NOTHING;
