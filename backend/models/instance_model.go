package models

import (
	"database/sql"
	"errors"
	"fmt"
)

type Instance struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type InstanceModel struct {
	DB *sql.DB
}

var ErrNotFound = errors.New("instance not found")

func (m *InstanceModel) GetInstanceByID(id int) (*Instance, error) {
	instance := &Instance{}
	query := `SELECT id, name, status FROM instances WHERE id = $1`

	row := m.DB.QueryRow(query, id)
	err := row.Scan(&instance.ID, &instance.Name, &instance.Status)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get instance %d: %w", id, err)

	}

	return instance, nil
}

func (m *InstanceModel) CreateInstance(name string) (*Instance, error) {
	instance := &Instance{}
	query := "INSERT INTO instances (name, status) VALUES ($1, 'active') RETURNING id, name, status"

	row := m.DB.QueryRow(query, name)
	err := row.Scan(&instance.ID, &instance.Name, &instance.Status)

	if err != nil {
		return nil, fmt.Errorf("create instance %q: %w", name, err)
	}

	return instance, nil
}

func (m *InstanceModel) UpdateStatus(id int, status string) (*Instance, error) {
	instance := &Instance{}
	query := "UPDATE instances SET status = $1 WHERE id = $2 RETURNING id, name, status"

	row := m.DB.QueryRow(query, status, id)
	err := row.Scan(&instance.ID, &instance.Name, &instance.Status)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("update instance %d status to %q: %w", id, status, err)
	}

	return instance, nil
}
