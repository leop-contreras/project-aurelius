CREATE TABLE challenges (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT NOT NULL
);

INSERT INTO challenges (name, description) VALUES
    ('Echo Chamber', 'The server only repeats what it was told. Launch it and see what it says.'),
    ('Port Hopper', 'Every instance gets its own port. Find yours and connect to it.'),
    ('Ticking Clock', 'Your instance lives for one hour. Renew it before it disappears.'),
    ('Hello Container', 'A tiny web server running in its own Docker container.'),
    ('Load Balanced', 'Each request is answered by a different worker. Watch the toasts.'),
    ('Last One Standing', 'Only five instances can run at once. Get yours before they run out.');

CREATE TABLE instances (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    status VARCHAR(50) NOT NULL,
    port INT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    renew_count INT NOT NULL DEFAULT 0,
    challenge_id INT NOT NULL REFERENCES challenges (id)
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
