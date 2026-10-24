package models

import (
	"database/sql"
	"main/events"
)

type HistoryModel struct {
	DB *sql.DB
}

func (m *HistoryModel) Record(event *events.InstanceEvent) (bool, error) {
	query := `INSERT INTO instance_history (event_id, event_type, instance_id, name, status, worker_id, occurred_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT (event_id) DO NOTHING`

	result, err := m.DB.Exec(query, event.EventID, event.Type, event.InstanceID, event.Name, event.Status, event.WorkerID, event.OccurredAt)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows > 0, err
}
