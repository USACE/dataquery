package goquery

import (
	"errors"
	"fmt"
	"io"
	"log"
	"reflect"
)

//@TODO panic on error is not complete
//implements the datastore interface

func NewRdbmsDataStore(config *RdbmsConfig) (DataStore, error) {
	switch config.DbStore {
	case "pgx":
		db, err := NewPgxConnection(config)
		if err != nil {
			return nil, errors.New(fmt.Sprintf("Unable to connect to pgx datastore: %s", err))
		}
		return &RdbmsDataStore{&db}, nil
	case "sqlx":
		db, err := NewSqlxConnection(config)
		if err != nil {
			return nil, errors.New(fmt.Sprintf("Unable to connect to sqlx datastore: %s", err))
		}
		return &RdbmsDataStore{&db}, nil
	default:
		return nil, errors.New(fmt.Sprintf("Unsupported store type: %s", config.DbStore))
	}
}

type RdbmsDataStore struct {
	db RdbmsDb
}

func (sds *RdbmsDataStore) RdbmsDb() RdbmsDb {
	return sds.db
}

func (sds *RdbmsDataStore) Connection() interface{} {
	return sds.db.Connection()
}

func (sds *RdbmsDataStore) NewTransaction() (Tx, error) {
	return sds.db.Transaction()
}

func (sds *RdbmsDataStore) Transaction(fn TransactionFunction) (err error) {
	var tx Tx
	tx, err = sds.NewTransaction()
	if err != nil {
		log.Printf("Unable to start transaction: %s\n", err)
		return err
	}
	defer func() {
		if r := recover(); r != nil {
			switch x := r.(type) {
			case string:
				err = errors.New(x)
			case error:
				err = x
			default:
				err = errors.New("unknown panic")
			}
			txerr := tx.Rollback()
			if txerr != nil {
				log.Printf("Unable to rollback from transaction: %s", err)
			}
		} else {
			err = tx.Commit()
			if err != nil {
				log.Printf("Unable to commit transaction: %s", err)
			}
		}
	}()
	fn(tx)
	return err
}

func (sds *RdbmsDataStore) Fetch(tx *Tx, qi QueryInput, qo QueryOutput, dest interface{}) error {
	sstmt, err := getSelectStatement(qi.DataSet, qi.StatementKey, qi.Statement, qi.Suffix, qi.StmtAppends, dest)
	if err != nil {
		return err
	}

	if qi.LogSql {
		log.Println(sstmt)
	}

	if qo.rowFunction != nil {
		rows, err := sds.FetchRows(tx, qi)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if dest != nil {
				err := rows.ScanStruct(dest)
				if err != nil {
					return err
				}
			}
			err = qo.rowFunction(rows)
			if err != nil {
				if qi.PanicOnErr {
					panic(err)
				}
				return err
			}
		}
		return nil
	} else {
		switch qo.OutputFormat {
		case JSON:
			return sds.GetJSON(qo.Writer, qi, qo.Options)
		case CSV:
			return errors.New("CSV is not implemented.")
			//return sds.GetCSV()
		default:
			if isSlice(dest) {
				err = sds.db.Select(dest, tx, sstmt, qi.BindParams...)
			} else {
				err = sds.db.Get(dest, tx, sstmt, qi.BindParams...)
			}
		}

		if err != nil && qi.PanicOnErr {
			panic(err)
		}
		return err
	}
}

func (sds *RdbmsDataStore) FetchRows(tx *Tx, qi QueryInput) (Rows, error) {
	sstmt, err := getSelectStatement(qi.DataSet, qi.StatementKey, qi.Statement, qi.Suffix, qi.StmtAppends, nil)
	if err != nil {
		return nil, err
	}
	return sds.db.Query(tx, sstmt, qi.BindParams...)
}

func (sds *RdbmsDataStore) GetJSON(writer io.Writer, qi QueryInput, jo OutputOptions) error {
	rows, err := sds.FetchRows(nil, qi)
	if err != nil {
		if qi.PanicOnErr {
			panic(err)
		}
		return err
	}
	defer rows.Close()

	return RowsToJSON(writer, rows, jo.ToCamelCase, jo.IsArray, jo.DateFormat, jo.OmitNull)
}

func (sds *RdbmsDataStore) GetCSV(qi QueryInput, co OutputOptions) (string, error) {
	rows, err := sds.FetchRows(nil, qi)
	if err != nil {
		log.Println(err)
		return "", err
	}
	defer rows.Close()
	return RowsToCSV(rows, co.ToCamelCase, co.DateFormat)
}

func (sds *RdbmsDataStore) InsertRecs(tx *Tx, input InsertInput) error {
	var err error
	recs := input.Records
	rval := reflect.ValueOf(recs)
	rrecs := reflect.Indirect(rval)
	if rrecs.Kind() == reflect.Slice {
		if input.Batch {
			err = sds.insertBatch(input.Dataset, rrecs, input.BatchSize)
		} else {
			if tx == nil {
				err = sds.insertNewTrans(input.Dataset, rrecs)
			} else {
				err = sds.insert(input.Dataset, rrecs, tx)
			}
		}
	} else {
		err = sds.db.Insert(input.Dataset, recs, tx)
	}
	if err != nil && input.PanicOnErr {
		panic(err)
	}
	return err
}

func (sds *RdbmsDataStore) Exec(tx *Tx, stmt string, params ...interface{}) error {
	return sds.db.Exec(tx, stmt, params...)
}

func (sds *RdbmsDataStore) Execr(tx *Tx, stmt string, params ...interface{}) (ExecResult, error) {
	return sds.db.Execr(tx, stmt, params...)
}

func (sds *RdbmsDataStore) MustExec(tx *Tx, stmt string, params ...interface{}) {
	sds.db.MustExec(tx, stmt, params...)
}

func (sds *RdbmsDataStore) MustExecr(tx *Tx, stmt string, params ...interface{}) ExecResult {
	return sds.db.MustExecr(tx, stmt, params...)
}

func (sds *RdbmsDataStore) insertNewTrans(ds DataSet, rrecs reflect.Value) error {
	err := sds.Transaction(func(tx Tx) {
		err := sds.insert(ds, rrecs, &tx)
		if err != nil {
			panic(err)
		}
	})
	return err
}

func (sds *RdbmsDataStore) insert(ds DataSet, rrecs reflect.Value, tx *Tx) error {
	for i := 0; i < rrecs.Len(); i++ {
		err := sds.db.Insert(ds, rrecs.Index(i).Interface(), tx)
		if err != nil {
			log.Printf("Failed to insert: %s\n", err)
			return err
		}
	}
	return nil
}

func (sds *RdbmsDataStore) insertBatch(ds DataSet, rrecs reflect.Value, batchSize int) error {
	batch, err := sds.db.Batch()
	if err != nil {
		return err
	}

	stmt, err := sds.db.InsertStmt(ds)
	if err != nil {
		return err
	}

	for i := 0; i < rrecs.Len(); i++ {
		rec := rrecs.Index(i).Interface()
		batch.Queue(stmt, StructToIArray(rec)...)
		if i >= batchSize {
			sds.db.SendBatch(batch)
			batch, err = sds.db.Batch()
			if err != nil {
				return err
			}
		}
	}
	sds.db.SendBatch(batch)
	return nil
}

func (sds *RdbmsDataStore) Select(stmt ...string) *FluentSelect {
	stmts := ""
	if len(stmt) > 0 && stmt[0] != "" {
		stmts = stmt[0]
	}
	s := FluentSelect{
		qi: QueryInput{
			Statement: stmts,
		},
		store: sds,
	}
	s.CamelCase(true)
	return &s
}

func (sds *RdbmsDataStore) Insert(ds DataSet) *FluentInsert {
	fi := FluentInsert{
		ds:    ds,
		store: sds,
	}
	return &fi
}
