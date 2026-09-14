package main

import (
	"database/sql"
	"errors"
	"fmt"
)

// changeStatus runs under the store's shared mutation lock. Reclassification
// touches only the registry, so it needs no filesystem intent record.
func (s *store) changeStatus(name, status string) (project, error) {
	var p project
	switch status {
	case "active", "completed", "abandoned":
	case "promoted":
		return p, fmt.Errorf("promoted can only be set through promotion")
	default:
		return p, fmt.Errorf("invalid status %q: use active, completed, or abandoned", status)
	}
	if err := s.reconcile(); err != nil {
		return p, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	err = tx.QueryRow(`SELECT name,created_date,status,location FROM projects WHERE name=?`, name).Scan(&p.Name, &p.Date, &p.Status, &p.Location)
	if errors.Is(err, sql.ErrNoRows) {
		return p, fmt.Errorf("unknown experimental project %q", name)
	}
	if err != nil {
		return p, err
	}
	if p.Status == "promoted" {
		return p, fmt.Errorf("experimental project %q is promoted: its status is terminal", name)
	}
	if _, err := tx.Exec(`UPDATE projects SET status=? WHERE name=?`, status, name); err != nil {
		return p, err
	}
	interrupt("during-status")
	if err := tx.Commit(); err != nil {
		return p, err
	}
	interrupt("after-status")
	p.Status = status
	return p, nil
}
