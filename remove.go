package main

import (
	"bufio"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
)

var trashDirectory = nativeTrash

var terminalInput = func() bool { return isatty.IsTerminal(os.Stdin.Fd()) }

func confirmRemoval(p project, missing, force bool) (bool, error) {
	if missing {
		fmt.Printf("Files are missing at %s; record-only removal of %q.\n", p.Location, p.Name)
	} else {
		fmt.Printf("Move %s to trash and end tracking of %q.\n", p.Location, p.Name)
	}
	if force {
		return true, nil
	}
	if !terminalInput() {
		return false, fmt.Errorf("noninteractive removal requires --force (confirmation only)")
	}
	fmt.Print("Continue? [y/N] ")
	answer, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("confirmation not received: %w", err)
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes", nil
}

// Deleting the record and its intent is atomic, including record-only removal.
func (s *store) completeRemoval(name, source string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`DELETE FROM projects WHERE name=? AND location=? AND status IN ('active','completed','abandoned')`, name, source)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("removal registry identity changed for %q; pending evidence preserved", name)
	}
	if _, err := tx.Exec(`DELETE FROM pending_removal WHERE singleton=1`); err != nil {
		return err
	}
	interrupt("during-removal-registry")
	return tx.Commit()
}

func (s *store) reconcileRemoval() error {
	var name, source, target, trashInfo string
	var expected directoryID
	var inFlight bool
	err := s.db.QueryRow(`SELECT name,source,target,device,inode,in_flight,trash_info FROM pending_removal WHERE singleton=1`).Scan(&name, &source, &target, &expected.device, &expected.inode, &inFlight, &trashInfo)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	actual, sourceErr := directoryIdentity(source)
	if !inFlight && target == "" && sourceErr == nil && actual == expected {
		_, err := s.db.Exec(`DELETE FROM pending_removal WHERE singleton=1`)
		return err
	}
	if target != "" && errors.Is(sourceErr, os.ErrNotExist) {
		trashed, err := directoryIdentity(target)
		if err == nil && trashed == expected {
			if receiptErr := syncTrashInfo(target, trashInfo); receiptErr != nil {
				return fmt.Errorf("%s: %w", uncertainRemoval(name, source, target, trashInfo), receiptErr)
			}
			if err := syncMoveParents(source, target); err != nil {
				return err
			}
			return s.completeRemoval(name, source)
		}
	}
	return errors.New(uncertainRemoval(name, source, target, trashInfo))
}

func (s *store) remove(name string, force bool) error {
	p, err := s.info(name)
	if err != nil {
		return err
	}
	if err := guardUnpromoted(p); err != nil {
		return err
	}
	identity, err := directoryIdentity(p.Location)
	missing := errors.Is(err, os.ErrNotExist)
	if err != nil && !missing {
		return err
	}
	consent, err := confirmRemoval(p, missing, force)
	if err != nil {
		return err
	}
	if !consent {
		fmt.Println("Removal declined; tracking and files unchanged.")
		return nil
	}
	if missing {
		if _, err := os.Lstat(p.Location); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("removal path changed: %s", p.Location)
		}
		if err := s.completeRemoval(name, p.Location); err != nil {
			return err
		}
	} else {
		actual, err := directoryIdentity(p.Location)
		if err != nil || actual != identity {
			return fmt.Errorf("removal path changed: %s", p.Location)
		}
		if _, err := s.db.Exec(`INSERT INTO pending_removal(singleton,name,source,device,inode) VALUES(1,?,?,?,?)`, name, p.Location, identity.device, identity.inode); err != nil {
			return err
		}
		interrupt("before-trash")
		// The native helper can outlive a killed Hatch process. Until it returns,
		// even an unchanged source cannot prove that the effect will not happen.
		if _, err := s.db.Exec(`UPDATE pending_removal SET in_flight=1 WHERE singleton=1`); err != nil {
			return err
		}
		interrupt("trash-in-flight")
		target, err := trashDirectory(p.Location, func(target, info string) error {
			_, err := s.db.Exec(`UPDATE pending_removal SET target=?,trash_info=? WHERE singleton=1`, target, info)
			return err
		})
		if err != nil {
			if _, dbErr := s.db.Exec(`UPDATE pending_removal SET in_flight=0 WHERE singleton=1`); dbErr != nil {
				return fmt.Errorf("trash failed: %w; could not record helper completion: %v; pending evidence retained", err, dbErr)
			}
			if recoveryErr := s.reconcileRemoval(); recoveryErr != nil {
				return fmt.Errorf("trash failed: %w; %v", err, recoveryErr)
			}
			return fmt.Errorf("trash failed; tracking retained: %w", err)
		}
		interrupt("after-trash")
		if _, err := s.db.Exec(`UPDATE pending_removal SET target=? WHERE singleton=1`, target); err != nil {
			return err
		}
		interrupt("after-trash-receipt")
		if err := s.reconcileRemoval(); err != nil {
			return err
		}
		fmt.Printf("Trashed to: %s\n", target)
	}
	interrupt("after-removal-registry")
	fmt.Printf("Removed %q.\n", name)
	return nil
}
