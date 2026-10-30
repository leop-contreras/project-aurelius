package models

import (
	"database/sql"
	"errors"
)

type Challenge struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type ChallengeModel struct {
	DB *sql.DB
}

var ErrChallengeNotFound = errors.New("challenge not found")

func (m *ChallengeModel) GetChallengeByID(id int) (*Challenge, error) {
	challenge := &Challenge{}
	err := m.DB.QueryRow("SELECT id, name, description FROM challenges WHERE id = $1", id).Scan(&challenge.ID, &challenge.Name, &challenge.Description)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrChallengeNotFound
	}
	if err != nil {
		return nil, err
	}

	return challenge, nil
}

func (m *ChallengeModel) ListChallenges() ([]*Challenge, error) {
	rows, err := m.DB.Query("SELECT id, name, description FROM challenges ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	challenges := []*Challenge{}
	for rows.Next() {
		challenge := &Challenge{}
		if err := rows.Scan(&challenge.ID, &challenge.Name, &challenge.Description); err != nil {
			return nil, err
		}
		challenges = append(challenges, challenge)
	}

	return challenges, rows.Err()
}
