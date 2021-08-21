package dataquery

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/georgysavva/scany/pgxscan"
	"github.com/jackc/pgtype"
	"github.com/jackc/pgx/v4"
)

type PgxRows struct {
	rows pgx.Rows
}

func (p PgxRows) Columns() ([]string, error) {
	metadata := p.rows.FieldDescriptions()
	columns := make([]string, len(metadata))
	for i, f := range metadata {
		columns[i] = string(f.Name)
	}
	return columns, nil
}

func (p PgxRows) ColumnTypes() ([]reflect.Type, error) {
	metadata := p.rows.FieldDescriptions()
	t := make([]reflect.Type, len(metadata))
	for i, fd := range metadata {
		switch fd.DataTypeOID {
		case pgtype.Float8OID:
			t[i] = reflect.TypeOf(float64(0))
		case pgtype.Float4OID:
			t[i] = reflect.TypeOf(float32(0))
		case pgtype.Int8OID:
			t[i] = reflect.TypeOf(int64(0))
		case pgtype.Int4OID:
			t[i] = reflect.TypeOf(int32(0))
		case pgtype.Int2OID:
			t[i] = reflect.TypeOf(int16(0))
		case pgtype.BoolOID:
			t[i] = reflect.TypeOf(false)
		case pgtype.NumericOID:
			t[i] = reflect.TypeOf(float64(0))
		case pgtype.DateOID, pgtype.TimestampOID, pgtype.TimestamptzOID:
			t[i] = reflect.TypeOf(time.Time{})
		case pgtype.ByteaOID:
			t[i] = reflect.TypeOf([]byte(nil))
		default:
			t[i] = reflect.TypeOf("")
		}
	}
	return t, nil
}

func (p PgxRows) Next() bool {
	return p.rows.Next()
}

func (p PgxRows) Scan(dest ...interface{}) error {
	return p.rows.Scan(dest...)
}

func (p PgxRows) Close() error {
	p.rows.Close()
	return nil
}

/*
type PgxBatch struct {
	BatchSize int
	batch     *pgx.Batch
}

func(pb *PgxBatch) Queue(stmt string, params ...interface{}){
	pb.batch.Queue(stmt,params)
}
*/

type PgxDb struct {
	db      *pgx.Conn
	dialect DbDialect
}

func NewPgxConnection(config *RdbmsConfig) (PgxDb, error) {
	dburl := fmt.Sprintf("user=%s password=%s host=%s port=%s database=%s sslmode=disable",
		config.Dbuser, config.Dbpass, config.Dbhost, config.Dbport, config.Dbname)
	con, err := pgx.Connect(context.Background(), dburl)
	return PgxDb{con, pgDialect}, err
}

func (pdb *PgxDb) Connection() interface{} {
	return pdb.db
}

func (pdb *PgxDb) Select(dest interface{}, stmt string, params ...interface{}) error {
	return pgxscan.Select(context.Background(), pdb.db, dest, stmt, params...)
}

func (pdb *PgxDb) Get(dest interface{}, stmt string, params ...interface{}) error {
	return pgxscan.Select(context.Background(), pdb.db, dest, stmt, params...)
}

func (pdb *PgxDb) Query(stmt string, params ...interface{}) (Rows, error) {
	rows, err := pdb.db.Query(context.Background(), stmt, params...)
	return PgxRows{rows}, err
}

func (pdb *PgxDb) Batch() Batch {
	return &pgx.Batch{}
}

func (pdb *PgxDb) SendBatch(batch Batch) BatchResult {
	pb := batch.(*pgx.Batch)
	br := pdb.db.SendBatch(context.Background(), pb)
	return br
}

func (pdb *PgxDb) Insert(ds DataSet, rec interface{}, tx *Tx) error {
	stmt, err := ToInsert(ds, pdb.dialect)
	if err != nil {
		return err
	}
	params := StructToIArray(rec)
	if tx == nil {
		_, err = pdb.db.Exec(context.Background(), stmt, params...)
		return err
	} else {
		pgxtx := tx.PgxTx()
		_, err = pgxtx.Exec(context.Background(), stmt, params...)
		return err
	}

}

func (pdb *PgxDb) Transaction() (Tx, error) {
	tx, err := pdb.db.Begin(context.Background())
	return Tx{tx}, err
}
