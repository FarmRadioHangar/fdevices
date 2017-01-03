package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
	// load ql drier
	_ "github.com/cznic/ql/driver"
)

//CtxKey is the key which is used to store the *sql.DB instance inside
//context.Context.
const CtxKey = "_db"

const migrationSQL = `
BEGIN TRANSACTION ;
	CREATE TABLE IF NOT EXISTS dongles(
		imei string,
		imsi string,
		path string,
		symlink bool,
		tty  int,
		properties blob,
		created_on time,
		updated_on time);

		CREATE UNIQUE INDEX UQE_dongels on dongles(path);
COMMIT;
`

//Dongle holds information about device dongles. This relies on combination from
//the information provided by udev and information that is gathered by talking
//to the device serial port directly.
type Dongle struct {
	IMEI        string            `json:"imei"`
	IMSI        string            `json:"imsi"`
	Path        string            `json:"path"`
	IsSymlinked bool              `json:"symlink"`
	TTY         int               `json:"-"`
	Properties  map[string]string `json:"properties"`

	CreatedOn time.Time `json:"-"`
	UpdatedOn time.Time `json:"-"`
}

//Migration creates necessary database tables if they aint created yet.
func Migration(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	_, err = tx.Exec(migrationSQL)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

//DB returns a ql backed database, with migrations already performed.
func DB() (*sql.DB, error) {
	db, err := sql.Open("ql-mem", "devices.db")
	if err != nil {
		return nil, err
	}
	err = Migration(db)
	if err != nil {
		return nil, err
	}
	return db, nil
}

func GetAllDongles(db *sql.DB) ([]*Dongle, error) {
	query := "select * from dongles"
	var rst []*Dongle
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		d := &Dongle{}
		var prop []byte
		err := rows.Scan(
			&d.IMEI,
			&d.IMSI,
			&d.Path,
			&d.IsSymlinked,
			&d.TTY,
			&prop,
			&d.CreatedOn,
			&d.UpdatedOn,
		)
		if err != nil {
			return nil, err
		}
		if prop != nil {
			err = json.Unmarshal(prop, &d.Properties)
			if err != nil {
				return nil, err
			}
		}
		rst = append(rst, d)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return rst, nil
}

func GetDistinc(db *sql.DB) ([]*Dongle, error) {
	query := `select distinc  imei,imsi, path,symlink, min(tty) AS tty,properties,
	created_on,updated on from dongles;
	`
	var rst []*Dongle
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		d := &Dongle{}
		var prop []byte
		err := rows.Scan(
			&d.IMEI,
			&d.IMSI,
			&d.Path,
			&d.IsSymlinked,
			&d.TTY,
			&prop,
			&d.CreatedOn,
			&d.UpdatedOn,
		)
		if err != nil {
			return nil, err
		}
		if prop != nil {
			err = json.Unmarshal(prop, &d.Properties)
			if err != nil {
				return nil, err
			}
		}
		rst = append(rst, d)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return rst, nil
}

func CreateDongle(db *sql.DB, d *Dongle) error {
	query := `
	BEGIN TRANSACTION;
	  INSERT INTO dongles  (imei,imsi,path,symlink,tty,properties,created_on,updated_on)
		VALUES ($1,$2,$3,$4,$5,$6,now(),now());
	COMMIT;
	`
	var prop []byte
	var err error
	if d.Properties != nil {
		prop, err = json.Marshal(d.Properties)
		if err != nil {
			return err
		}
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}

	_, err = tx.Exec(query, d.IMEI, d.IMSI, d.Path, d.IsSymlinked, d.TTY, prop)
	if err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func RemoveDongle(db *sql.DB, d *Dongle) error {
	var query = `
BEGIN TRANSACTION;
   DELETE FROM dongles
  WHERE imei=$1&&path=$2;
COMMIT;
	`
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	_, err = tx.Exec(query, d.IMEI, d.Path)
	if err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
	return nil
}

func GetDongle(db *sql.DB, path string) (*Dongle, error) {
	var query = `
	SELECT * from dongles  WHERE path=$1 LIMIT 1;
	`
	d := &Dongle{}
	var prop []byte
	err := db.QueryRow(query, path).Scan(
		&d.IMEI,
		&d.IMSI,
		&d.Path,
		&d.IsSymlinked,
		&d.TTY,
		&prop,
		&d.CreatedOn,
		&d.UpdatedOn,
	)
	if err != nil {
		return nil, err
	}
	if prop != nil {
		err = json.Unmarshal(prop, &d.Properties)
		if err != nil {
			return nil, err
		}
	}
	return d, nil
}

func GetSymlinkCandidate(db *sql.DB, imei string) (*Dongle, error) {
	query := "select  min(tty) from dongles where imei=$1 "
	var tty int
	err := db.QueryRow(query, imei).Scan(&tty)
	if err != nil {

		return nil, err
	}
	path := fmt.Sprintf("/dev/ttyUSB%d", tty)
	return GetDongle(db, path)
}
