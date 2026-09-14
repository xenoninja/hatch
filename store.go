package main

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"syscall"

	_ "modernc.org/sqlite"
)

type project struct{ Name, Date, Status, Location string }
type store struct {
	db          *sql.DB
	lock        *os.File
	experiments string
}

func openStore(home string, create bool) (*store, error) {
	root := filepath.Join(home, ".local", "share", "hatch")
	path := filepath.Join(root, "hatch.db")
	if !create {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return nil, nil
		} else if err != nil {
			return nil, err
		}
	}
	if create {
		if err := makeDurableDirectories(home, root, 0700); err != nil {
			return nil, err
		}
	}
	lock, err := os.OpenFile(filepath.Join(root, "mutation.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		lock.Close()
		return nil, err
	}
	mode := "rw"
	if create {
		mode = "rwc"
	}
	uri := url.URL{Scheme: "file", Path: path}
	db, err := sql.Open("sqlite", uri.String()+"?mode="+mode)
	if err != nil {
		lock.Close()
		return nil, err
	}
	s := &store{db: db, lock: lock, experiments: filepath.Join(home, "experiments")}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`PRAGMA synchronous=FULL; PRAGMA busy_timeout=5000;`); err != nil {
		s.close()
		return nil, err
	}
	if create {
		_, err = db.Exec(`CREATE TABLE IF NOT EXISTS projects (
   sequence INTEGER PRIMARY KEY AUTOINCREMENT,
   name TEXT NOT NULL UNIQUE, created_date TEXT NOT NULL,
   status TEXT NOT NULL, location TEXT NOT NULL UNIQUE
  );
  CREATE TABLE IF NOT EXISTS pending_creation (
   singleton INTEGER PRIMARY KEY CHECK(singleton=1), name TEXT NOT NULL,
   created_date TEXT NOT NULL, location TEXT NOT NULL, device TEXT, inode TEXT
  );`)
		if err != nil {
			s.close()
			return nil, err
		}
	}
	if create {
		if err := syncDirectory(root); err != nil {
			s.close()
			return nil, err
		}
	}
	return s, nil
}

func (s *store) close() { s.db.Close(); s.lock.Close() }

func (s *store) info(name string) (project, error) {
	var p project
	err := s.db.QueryRow(`SELECT name,created_date,status,location FROM projects WHERE name=?`, name).Scan(&p.Name, &p.Date, &p.Status, &p.Location)
	if errors.Is(err, sql.ErrNoRows) {
		return p, fmt.Errorf("unknown experimental project %q", name)
	}
	return p, err
}

// Sync all ancestors through home, including existing ones: an earlier
// invocation may have stopped between mkdir and syncing its parent.
// Both callers construct path strictly beneath home.
func makeDurableDirectories(home, path string, mode os.FileMode) error {
	if err := os.MkdirAll(path, mode); err != nil {
		return err
	}
	interrupt("before-storage-sync")
	for current := path; ; current = filepath.Dir(current) {
		if err := syncDirectory(current); err != nil {
			return err
		}
		if current == home {
			return nil
		}
	}
}

func syncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

type directoryID struct{ device, inode string }

func directoryIdentity(path string) (directoryID, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return directoryID{}, err
	}
	if !info.IsDir() {
		return directoryID{}, fmt.Errorf("not a directory: %s", path)
	}
	stat := info.Sys().(*syscall.Stat_t)
	return directoryID{fmt.Sprint(stat.Dev), fmt.Sprint(stat.Ino)}, nil
}

func (s *store) create(name, date string) (project, error) {
	p := project{name, date, "active", filepath.Join(s.experiments, date+"-"+name)}
	var count int
	if err := s.db.QueryRow(`SELECT count(*) FROM projects WHERE name=?`, name).Scan(&count); err != nil {
		return p, err
	}
	if count != 0 {
		return p, fmt.Errorf("experimental project %q is already tracked", name)
	}
	if _, err := os.Lstat(p.Location); err == nil {
		return p, fmt.Errorf("destination already exists: %s", p.Location)
	} else if !errors.Is(err, os.ErrNotExist) {
		return p, err
	}
	if err := makeDurableDirectories(filepath.Dir(s.experiments), s.experiments, 0755); err != nil {
		return p, err
	}
	if _, err := s.db.Exec(`INSERT INTO pending_creation(singleton,name,created_date,location) VALUES(1,?,?,?)`, name, date, p.Location); err != nil {
		return p, err
	}
	interrupt("before-directory")
	if err := os.Mkdir(p.Location, 0755); err != nil {
		// Keep intent if the path appeared: ownership is uncertain and recovery must block.
		return p, fmt.Errorf("create %s: %w", p.Location, err)
	}
	interrupt("after-directory")
	if err := syncDirectory(p.Location); err != nil {
		return p, err
	}
	if err := syncDirectory(s.experiments); err != nil {
		return p, err
	}
	identity, err := directoryIdentity(p.Location)
	if err != nil {
		return p, err
	}
	if _, err := s.db.Exec(`UPDATE pending_creation SET device=?,inode=? WHERE singleton=1`, identity.device, identity.inode); err != nil {
		return p, err
	}
	interrupt("before-registry")
	if err := s.completeCreation(p); err != nil {
		return p, err
	}
	interrupt("after-registry")
	return p, nil
}

func (s *store) completeCreation(p project) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO projects(name,created_date,status,location) VALUES(?,?,?,?)`, p.Name, p.Date, p.Status, p.Location); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM pending_creation WHERE singleton=1`); err != nil {
		return err
	}
	interrupt("during-registry")
	return tx.Commit()
}

func (s *store) reconcile() error {
	var p project
	var dev, ino sql.NullString
	err := s.db.QueryRow(`SELECT name,created_date,location,device,inode FROM pending_creation WHERE singleton=1`).Scan(&p.Name, &p.Date, &p.Location, &dev, &ino)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	actual, err := directoryIdentity(p.Location)
	if errors.Is(err, os.ErrNotExist) && !dev.Valid && !ino.Valid {
		_, err = s.db.Exec(`DELETE FROM pending_creation WHERE singleton=1`)
		return err
	}
	if err != nil || !dev.Valid || !ino.Valid || actual != (directoryID{dev.String, ino.String}) {
		return fmt.Errorf("uncertain interrupted creation of %q at %s; files preserved, mutations blocked (pending evidence in registry); manual investigation required", p.Name, p.Location)
	}
	p.Status = "active"
	return s.completeCreation(p)
}
