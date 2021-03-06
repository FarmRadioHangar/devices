package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"time"
	// load ql drier
	"github.com/FarmRadioHangar/fdevices/log"
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
		ati string,
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
	ATI         string            `json:"ati"`
	Properties  map[string]string `json:"properties"`

	CreatedOn time.Time `json:"-"`
	UpdatedOn time.Time `json:"-"`
}

type Dongles []*Dongle

func (a Dongles) Len() int           { return len(a) }
func (a Dongles) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a Dongles) Less(i, j int) bool { return a[i].TTY < a[j].TTY }

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
	return dbWIthName("devices.db")
}

func dbWIthName(name string) (*sql.DB, error) {
	db, err := sql.Open("ql-mem", name)
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
			&d.ATI,
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

func GetDistinct(db *sql.DB) ([]*Dongle, error) {
	s := make(map[string]Dongles)
	a, err := GetAllDongles(db)
	if err != nil {
		return nil, err
	}
	if len(a) == 0 {
		return a, nil
	}
	for k := range a {
		if v, ok := s[a[k].IMEI]; ok {
			v = append(v, a[k])
			s[a[k].IMEI] = v
		}
		s[a[k].IMEI] = Dongles{a[k]}
	}
	for k, v := range s {
		sort.Sort(v)
		s[k] = v
	}
	var out []*Dongle
	for _, v := range s {
		out = append(out, v[0])
	}
	return out, nil
}

func CreateDongle(db *sql.DB, d *Dongle) error {
	query := `
	BEGIN TRANSACTION;
	  INSERT INTO dongles  (imei,imsi,path,symlink,tty,ati,properties,created_on,updated_on)
		VALUES ($1,$2,$3,$4,$5,$6,$7,now(),now());
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

	_, err = tx.Exec(query, d.IMEI, d.IMSI,
		d.Path, d.IsSymlinked, d.TTY, d.ATI, prop)
	if err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func UpdateDongle(db *sql.DB, d *Dongle) error {
	query := `
	BEGIN TRANSACTION;
	  UPDATE dongles
	  imei=$1,imsi=$2 ,path=$3,symlink=$4,
	  tty=$5,properties=$6,
	  created_on=$7 ,updated_on=now(),
	  WHERE path=$3&&imei=$1;
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

	_, err = tx.Exec(query, d.IMEI, d.IMSI, d.Path, d.IsSymlinked, d.TTY, prop, d.CreatedOn)
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
  WHERE imei=$1;
COMMIT;
	`
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	_, err = tx.Exec(query, d.IMEI)
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
		&d.ATI,
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

func GetDongleByIMEI(db *sql.DB, imei string) (*Dongle, error) {
	var query = `
	SELECT * from dongles  WHERE imei=$1 LIMIT 1;
	`
	d := &Dongle{}
	var prop []byte
	err := db.QueryRow(query, imei).Scan(
		&d.IMEI,
		&d.IMSI,
		&d.Path,
		&d.IsSymlinked,
		&d.TTY,
		&d.ATI,
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

// GetSymlinkCandidate returns the dongle with the lowest tty number
func GetSymlinkCandidate(db *sql.DB, imei string) (*Dongle, error) {
	query := `select  min(tty) from dongles where imei=$1 `
	var tty int
	err := db.QueryRow(query, imei).Scan(&tty)
	if err != nil {
		return nil, err
	}
	path := fmt.Sprintf("/dev/ttyUSB%d", tty)
	return GetDongle(db, path)
}

// DongleExists return true when the dongle DongleExists
func DongleExists(db *sql.DB, modem *Dongle) bool {
	query := `select  count(*) from dongles where imei=$1&&imsi=$2&&path=$3 `
	var count int
	err := db.QueryRow(query,
		modem.IMEI,
		modem.IMSI,
		modem.Path,
	).Scan(&count)
	if err != nil {
		log.Error(err.Error())
		return false
	}
	return count > 0
}
