package dataquery

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"reflect"
	"strings"

	"github.com/jmoiron/sqlx"
)

type SqlDataStore struct {
	DB                *sqlx.DB
	Config            *RdbmsConfig
	SequenceTemplate  SequenceTemplateFunction
	BindParamTemplate BindParamTemplateFunction
}

func NewSqlConnection(config *RdbmsConfig) (*sqlx.DB, error) {
	dburl := fmt.Sprintf("user=%s password=%s host=%s port=%s database=%s sslmode=disable",
		config.Dbuser, config.Dbpass, config.Dbhost, config.Dbport, config.Dbname)
	con, err := sqlx.Connect("pgx", dburl)
	return con, err
}

/*
func NewSqlDataStore(config *config.AppConfig) (*SqlDataStore, error) {
	con, err := NewSqlConnection(config)
	if err != nil {
		return nil, err
	}
	con.SetMaxOpenConns(10)
	sds := SqlDataStore{
		DB:     con,
		Config: config,
		sequenceTemplate: func(seq string) string {
			return fmt.Sprintf("nextval('%s')", seq)
		},
		bindParamTemplate: func(field string, i int) string {
			return fmt.Sprintf(":%s", field)
		},
	}
	test := sds.sequenceTemplate("asdfasdf")
	fmt.Println(test)
	return &sds, nil
}
*/

func (sds *SqlDataStore) Connection() interface{} {
	return sds.DB
}

func (sds *SqlDataStore) BeginTransaction() (Tx, error) {
	tx, err := sds.DB.Beginx()
	return Tx{tx}, err
}

func (sds *SqlDataStore) GetSlice(ds DataSet, key string, stmt string, suffix string, params []interface{}) (interface{}, error) {
	sstmt, err := getSelectStatement(ds, key, stmt, suffix)
	if err != nil {
		return nil, err
	}
	data := ds.FieldSlice()
	if len(params) > 0 && params[0] != nil {
		err = sds.DB.Select(data, sstmt, params...)
	} else {
		err = sds.DB.Select(data, sstmt)
	}
	return data, err
}

func (sds *SqlDataStore) GetRecord(ds DataSet, key string, stmt string, suffix string, params []interface{}) (interface{}, error) {
	sstmt, err := getSelectStatement(ds, key, stmt, suffix)
	if err != nil {
		return nil, err
	}
	typ := reflect.TypeOf(ds.Attributes())
	data := reflect.New(typ).Interface()
	if len(params) > 0 && params[0] != nil {
		err = sds.DB.Get(data, sstmt, params...)
	} else {
		err = sds.DB.Get(data, sstmt)
	}
	return data, err
}

func (sds *SqlDataStore) GetJSON(ds DataSet, key string, stmt string, suffix string, params []interface{}, toCamelCase bool, forceArray bool) ([]byte, error) {
	sstmt, err := getSelectStatement(ds, key, stmt, suffix)
	if err != nil {
		return nil, err
	}
	var rows *sql.Rows
	if len(params) > 0 && params[0] != nil {
		rows, err = sds.DB.Query(sstmt, params...)
	} else {
		rows, err = sds.DB.Query(sstmt)
	}
	if err != nil {
		log.Println(err)
		log.Println(sstmt)
		return nil, err
	}
	defer rows.Close()
	return RowsToJSON(rows, toCamelCase, forceArray)
}

func (sds *SqlDataStore) GetCSV(ds DataSet, key string, stmt string, suffix string, params []interface{}, toCamelCase bool, forceArray bool) (string, error) {
	sstmt, err := getSelectStatement(ds, key, stmt, suffix)
	if err != nil {
		return "", err
	}
	var rows *sql.Rows
	if len(params) > 0 && params[0] != nil {
		rows, err = sds.DB.Query(sstmt, params...)
	} else {
		rows, err = sds.DB.Query(sstmt)
	}
	if err != nil {
		log.Println(err)
		log.Println(sstmt)
		return "", err
	}
	defer rows.Close()
	return RowsToCSV(rows, toCamelCase)
}

func (sds *SqlDataStore) Select(ds DataSet) *FluentSelect {
	s := FluentSelect{
		dataSet: ds,
		store:   sds,
	}
	s.CamelCase(true)
	return &s
}

func (sds *SqlDataStore) Insert(ds DataSet, val interface{}, retval interface{}, tx *sqlx.Tx) error {
	var err error
	if retval != nil {
		stmt, err := ToInsert(ds, sds.SequenceTemplate, func(field string, i int) string { return fmt.Sprintf("$%d", i) })
		if err != nil {
			return err
		}
		stmt = stmt + " returning id"
		fmt.Println(stmt)
		if tx == nil {
			err = sds.DB.Get(retval, stmt, ValsAsInterfaceArray2(val, []string{"ID"}, "db", []string{"_"})...)
		} else {
			err = tx.Get(retval, stmt, ValsAsInterfaceArray2(val, []string{"ID"}, "db", []string{"_"})...)
		}
		if err != nil {
			return err
		}
	} else {
		stmt, err := ToInsert(ds, sds.SequenceTemplate, sds.BindParamTemplate)
		if err != nil {
			return err
		}
		fmt.Println(stmt)
		_, err = sds.DB.NamedExec(stmt, val)
		if err != nil {
			return err
		}
	}
	//@TODO this error is getting shadowed by the inner error...need to fix
	return err
}

func (sds *SqlDataStore) Update(ds DataSet, val interface{}) error {
	stmt := ToUpdate(ds, sds.BindParamTemplate)
	fmt.Println(stmt)
	_, err := sds.DB.NamedExec(stmt, val)
	return err
}

func (sds *SqlDataStore) Delete(ds DataSet, id interface{}) error {
	template := "delete from %s where %s = $1"
	idfield := IdField(ds)
	stmt := fmt.Sprintf(template, ds.Entity(), idfield)
	_, err := sds.DB.Exec(stmt, id)
	return err
}

func getSelectStatement(ds DataSet, key string, stmt string, suffix string) (string, error) {
	switch {
	case key != "":
		if stmt, ok := ds.Commands()[key]; ok {
			return fmt.Sprintf("%s %s", stmt, suffix), nil
		}
		return "", errors.New(fmt.Sprintf("Unable to find statement for %s: %s", ds.Entity(), key))
	case stmt != "":
		return fmt.Sprintf("%s %s", stmt, suffix), nil
	default:
		if stmt, ok := ds.Commands()[selectkey]; ok {
			return fmt.Sprintf("%s %s", stmt, suffix), nil
		} else {
			stmt = ToSelectStmt(ds)
			ds.PutCommand(selectkey, stmt)
			return fmt.Sprintf("%s %s", stmt, suffix), nil
		}
	}
}

func ToSelectStmt(ds DataSet) string {
	fmt.Println("Building Statement")
	var fieldsBuilder strings.Builder
	fieldsBuilder.WriteString("select ")
	typ := reflect.TypeOf(ds.Attributes())
	fieldNum := typ.NumField()
	field := 0
	for i := 0; i < fieldNum; i++ {
		if tagval, ok := typ.Field(i).Tag.Lookup("db"); ok && tagval != "_" {
			if field > 0 {
				fieldsBuilder.WriteRune(',')
			}
			fieldsBuilder.WriteString(tagval)
			field++
		}
	}
	fieldsBuilder.WriteString(fmt.Sprintf(" from %s", ds.Entity()))
	return fieldsBuilder.String()
}

func ToInsert(ds DataSet, seqTemplate SequenceTemplateFunction, bindTemplate BindParamTemplateFunction) (string, error) {
	var fieldBuilder strings.Builder
	var bindBuilder strings.Builder
	typ := reflect.TypeOf(ds.Attributes())
	fieldNum := typ.NumField()
	fieldcount := 0
	for i := 0; i < fieldNum; i++ {
		if dbfield, ok := typ.Field(i).Tag.Lookup("db"); ok && dbfield != "_" {
			if fieldcount > 0 {
				fieldBuilder.WriteRune(',')
				bindBuilder.WriteRune(',')
			}
			if idtype, ok := typ.Field(i).Tag.Lookup("dbid"); ok {
				if idtype != "AUTOINCREMENT" {
					if idsequence, ok := typ.Field(i).Tag.Lookup("idsequence"); ok {
						fieldBuilder.WriteString(dbfield)
						bindBuilder.WriteString(seqTemplate(idsequence))
						fieldcount++
					} else {
						return "", errors.New("Invalid id.  Sequence type must have an 'idsequence' tag")
					}
				}
			} else {
				fieldBuilder.WriteString(dbfield)
				bindBuilder.WriteString(bindTemplate(dbfield, fieldcount))
				fieldcount++
			}

		}
	}
	return fmt.Sprintf("insert into %s (%s) values (%s)", ds.Entity(), fieldBuilder.String(), bindBuilder.String()), nil
}

func ToUpdate(ds DataSet, bindTemplateFunction BindParamTemplateFunction) string {
	var fieldsBuilder strings.Builder
	var criteria string
	typ := reflect.TypeOf(ds.Attributes())
	fieldNum := typ.NumField()
	field := 0
	for i := 0; i < fieldNum; i++ {
		if tagval, ok := typ.Field(i).Tag.Lookup("db"); ok && tagval != "_" {
			if field > 0 {
				fieldsBuilder.WriteRune(',')
			}
			if _, ok := typ.Field(i).Tag.Lookup("dbid"); ok {
				criteria = fmt.Sprintf(" where %s = %s", tagval, bindTemplateFunction(tagval, field))
				continue //skip id field
			}
			fieldsBuilder.WriteString(fmt.Sprintf("%s = %s", tagval, bindTemplateFunction(tagval, field)))
			field++
		}
	}
	return fmt.Sprintf("update %s set %s %s", ds.Entity(), fieldsBuilder.String(), criteria)
}

func IdField(ds DataSet) string {
	typ := reflect.TypeOf(ds.Attributes())
	fieldNum := typ.NumField()
	for i := 0; i < fieldNum; i++ {
		if tagval, ok := typ.Field(i).Tag.Lookup("db"); ok {
			if _, ok := typ.Field(i).Tag.Lookup("dbid"); ok {
				return tagval
			}
		}
	}
	return ""
}
