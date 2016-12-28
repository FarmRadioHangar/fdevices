package devices

import (
	"database/sql"
	// load ql drier
	_ "github.com/cznic/ql/driver"
)

const migrationSQL = `
BEGIN TRANSACTION ;
	CREATE TABLE IF NOT EXISTS dongles(
		imei string,
		imsi string,
		path string,
		properties blob,
		created_on time,
		updated_on time);


		CREATE UNIQUE INDEX UQE_dongels on dongles(imei,imsi);
COMMIT;
`

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
