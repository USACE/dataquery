package dataquery

type RecordHandler func(interface{}) error

type BindParamTemplateFunction func(field string, i int) string
type SequenceTemplateFunction func(sequence string) string

type DataStore interface {
	Connection() interface{}
	GetSlice(ds DataSet, key string, stmt string, suffix string, params []interface{}) (interface{}, error)
	GetRecord(ds DataSet, key string, stmt string, suffix string, params []interface{}) (interface{}, error)
	GetJSON(ds DataSet, key string, stmt string, suffix string, params []interface{}, toCamelCase bool, forceArray bool) ([]byte, error)
	GetCSV(ds DataSet, key string, stmt string, suffix string, params []interface{}, toCamelCase bool, forceArray bool) (string, error)
	Select(ds DataSet) *FluentSelect
	//RecordsetIterator(s Select, handler RecordHandler)
	BeginTransaction() (Tx, error)
}
