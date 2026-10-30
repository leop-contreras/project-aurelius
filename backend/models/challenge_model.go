package models

import (
	"database/sql"
)

type Challenge struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type ChallengeModel struct {
	DB *sql.DB
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
