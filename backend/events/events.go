package events

import "time"

const (
	TopicInstanceCreated       = "instances.created"
	TopicInstanceStatusUpdated = "instances.status-updated"
)

type InstanceEvent struct {
	EventID    string    `json:"event_id"`
	Type       string    `json:"type"`
	InstanceID int       `json:"instance_id"`
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	WorkerID   string    `json:"worker_id"`
	OccurredAt time.Time `json:"occurred_at"`
}
