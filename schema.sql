CREATE TABLE instances (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    status VARCHAR(50) NOT NULL
);

INSERT INTO instances (name, status) VALUES
('Instance 1', 'active'),
('Instance 2', 'inactive');