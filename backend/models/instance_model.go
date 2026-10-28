package models

import (
	"database/sql"
	"errors"
	"time"

	"github.com/lib/pq"
)

const (
	Timeout   = 3600
	MaxRenews = 5
	MinPort   = 10000
	MaxPort   = 10100
)

type Instance struct {
	ID         int       `json:"id"`
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	Port       int       `json:"port"`
	ExpiresAt  time.Time `json:"expires_at"`
	RenewCount int       `json:"renew_count"`
}

type InstanceModel struct {
	DB *sql.DB
}

var ErrNotFound = errors.New("instance not found")
var ErrNoPorts = errors.New("no free ports")
var ErrCannotRenew = errors.New("instance cannot be renewed")

const columns = "id, name, status, port, expires_at, renew_count"

func scan(row interface{ Scan(...any) error }) (*Instance, error) {
	instance := &Instance{}
	err := row.Scan(&instance.ID, &instance.Name, &instance.Status, &instance.Port, &instance.ExpiresAt, &instance.RenewCount)
	if err != nil {
		return nil, err
	}
	return instance, nil
}

func (m *InstanceModel) GetInstanceByID(id int) (*Instance, error) {
	query := "SELECT " + columns + " FROM instances WHERE id = $1"

	instance, err := scan(m.DB.QueryRow(query, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	return instance, nil
}

func (m *InstanceModel) ListInstances() ([]*Instance, error) {
	rows, err := m.DB.Query("SELECT " + columns + " FROM instances ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	instances := []*Instance{}
	for rows.Next() {
		instance, err := scan(rows)
		if err != nil {
			return nil, err
		}
		instances = append(instances, instance)
	}

	return instances, rows.Err()
}

func (m *InstanceModel) CountActive() (int, error) {
	var count int
	err := m.DB.QueryRow("SELECT count(*) FROM instances WHERE status = 'active'").Scan(&count)
	return count, err
}

func (m *InstanceModel) CreateInstance(name string) (*Instance, error) {
	query := `INSERT INTO instances (name, status, port, expires_at)
		SELECT $1, 'active', p, now() + make_interval(secs => $2)
		FROM generate_series($3::int, $4::int) AS p
		WHERE p NOT IN (SELECT port FROM instances WHERE status = 'active')
		ORDER BY p LIMIT 1
		RETURNING ` + columns

	var err error
	for attempt := 1; attempt <= 3; attempt++ {
		var instance *Instance
		instance, err = scan(m.DB.QueryRow(query, name, Timeout, MinPort, MaxPort))
		if err == nil {
			return instance, nil
		}

		var pqErr *pq.Error
		if !errors.As(err, &pqErr) || pqErr.Code != "23505" {
			break
		}
	}

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoPorts
	}
	return nil, err
}

func (m *InstanceModel) DeleteInstance(id int) error {
	_, err := m.DB.Exec("DELETE FROM instances WHERE id = $1", id)
	return err
}

func (m *InstanceModel) UpdateStatus(id int, status string) (*Instance, error) {
	query := "UPDATE instances SET status = $1 WHERE id = $2 RETURNING " + columns
	return scan(m.DB.QueryRow(query, status, id))
}

func (m *InstanceModel) RenewInstance(id int) (*Instance, error) {
	query := `UPDATE instances SET expires_at = now() + make_interval(secs => $2), renew_count = renew_count + 1
		WHERE id = $1 AND status = 'active' AND renew_count < $3
		RETURNING ` + columns

	instance, err := scan(m.DB.QueryRow(query, id, Timeout, MaxRenews))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCannotRenew
	}
	return instance, err
}

func (m *InstanceModel) GetExpired() ([]*Instance, error) {
	rows, err := m.DB.Query("SELECT " + columns + " FROM instances WHERE status = 'active' AND expires_at < now()")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	instances := []*Instance{}
	for rows.Next() {
		instance, err := scan(rows)
		if err != nil {
			return nil, err
		}
		instances = append(instances, instance)
	}

	return instances, rows.Err()
}
