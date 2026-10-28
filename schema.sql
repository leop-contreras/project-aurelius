CREATE TABLE instances (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    status VARCHAR(50) NOT NULL,
    port INT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    renew_count INT NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX instances_active_port ON instances (port) WHERE status = 'active';

CREATE TABLE instance_history (
    id SERIAL PRIMARY KEY,
    event_id VARCHAR(64) NOT NULL UNIQUE,
    event_type VARCHAR(50) NOT NULL,
    instance_id INT NOT NULL,
    name VARCHAR(255) NOT NULL,
    status VARCHAR(50) NOT NULL,
    worker_id VARCHAR(10) NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
