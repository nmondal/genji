package database

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/genjidb/genji/document"
	"github.com/genjidb/genji/document/encoding"
	"github.com/genjidb/genji/engine"
	"github.com/genjidb/genji/index"
)

// TableConfig holds the configuration of a table
type TableConfig struct {
	FieldConstraints []FieldConstraint
}

// ToDocument returns a document from t.
func (t *TableConfig) ToDocument() document.Document {
	buf := document.NewFieldBuffer()

	vbuf := document.NewValueBuffer()
	for _, fc := range t.FieldConstraints {
		vbuf = vbuf.Append(document.NewDocumentValue(fc.ToDocument()))
	}

	buf.Add("field_constraints", document.NewArrayValue(vbuf))
	return buf
}

// ScanDocument implements the document.Scanner interface.
func (t *TableConfig) ScanDocument(d document.Document) error {
	v, err := d.GetByField("field_constraints")
	if err != nil {
		return err
	}
	ar, err := v.ConvertToArray()
	if err != nil {
		return err
	}

	l, err := document.ArrayLength(ar)
	if err != nil {
		return err
	}

	t.FieldConstraints = make([]FieldConstraint, l)

	return ar.Iterate(func(i int, value document.Value) error {
		doc, err := value.ConvertToDocument()
		if err != nil {
			return err
		}
		return t.FieldConstraints[i].ScanDocument(doc)
	})
}

// GetPrimaryKey returns the field constraint of the primary key.
// Returns nil if there is no primary key.
func (t TableConfig) GetPrimaryKey() *FieldConstraint {
	for _, f := range t.FieldConstraints {
		if f.IsPrimaryKey {
			return &f
		}
	}

	return nil
}

// FieldConstraint describes constraints on a particular field.
type FieldConstraint struct {
	Path         document.ValuePath
	Type         document.ValueType
	IsPrimaryKey bool
	IsNotNull    bool
}

// ToDocument returns a document from f.
func (f *FieldConstraint) ToDocument() document.Document {
	buf := document.NewFieldBuffer()

	buf.Add("path", document.NewArrayValue(valuePathToArray(f.Path)))
	buf.Add("type", document.NewIntValue(int(f.Type)))
	buf.Add("is_primary_key", document.NewBoolValue(f.IsPrimaryKey))
	buf.Add("is_not_null", document.NewBoolValue(f.IsNotNull))
	return buf
}

// ScanDocument implements the document.Scanner interface.
func (f *FieldConstraint) ScanDocument(d document.Document) error {
	v, err := d.GetByField("path")
	if err != nil {
		return err
	}
	f.Path, err = arrayToValuePath(v)
	if err != nil {
		return err
	}

	v, err = d.GetByField("type")
	if err != nil {
		return err
	}
	tp, err := v.ConvertToInt64()
	if err != nil {
		return err
	}
	f.Type = document.ValueType(tp)

	v, err = d.GetByField("is_primary_key")
	if err != nil {
		return err
	}
	f.IsPrimaryKey, err = v.ConvertToBool()
	if err != nil {
		return err
	}

	v, err = d.GetByField("is_not_null")
	if err != nil {
		return err
	}
	f.IsNotNull, err = v.ConvertToBool()
	return err
}

type tableInfo struct {
	// storeID is a generated ID that acts as a key to reference this table.
	// The first-4 bytes represents the timestamp in second and the last-2 bytes are
	// randomly generated.
	storeID [6]byte

	cfg *TableConfig
}

func (ti *tableInfo) ToDocument() document.Document {
	buf := document.NewFieldBuffer()

	buf.Add("storeID", document.NewBlobValue(ti.storeID[:]))
	buf.Add("config", document.NewDocumentValue(ti.cfg.ToDocument()))

	return buf
}

func (ti *tableInfo) ScanDocument(d document.Document) error {
	v, err := d.GetByField("storeID")
	if err != nil {
		return err
	}
	b, err := v.ConvertToBlob()
	if err != nil {
		return err
	}
	copy(ti.storeID[:], b)

	v, err = d.GetByField("config")
	if err != nil {
		return err
	}
	doc, err := v.ConvertToDocument()
	if err != nil {
		return err
	}

	ti.cfg = &TableConfig{}
	return ti.cfg.ScanDocument(doc)
}

type tableInfoStore struct {
	st engine.Store
}

// Insert inserts a new tableInfo for the table tableName.
// If the generated storeID already exists, a new one will be generated
// until it doesn't already exist.
func (t *tableInfoStore) Insert(tableName string, cfg TableConfig) (*tableInfo, error) {
	key := []byte(tableName)
	_, err := t.st.Get(key)
	if err == nil {
		return nil, ErrTableAlreadyExists
	}
	if err != engine.ErrKeyNotFound {
		return nil, err
	}

	var id [6]byte
	for {
		id = generateStoreID()
		_, err = t.st.Get(id[:])
		if err == nil {
			// A store with this id already exists.
			// Let's generate a new one.
			continue
		}
		if err != engine.ErrKeyNotFound {
			return nil, err
		}
		break
	}
	ti := tableInfo{
		storeID: id,
		cfg:     &cfg,
	}

	v, err := encoding.EncodeDocument(ti.ToDocument())
	if err != nil {
		return nil, err
	}

	err = t.st.Put(key, v)
	if err != nil {
		return nil, err
	}
	return &ti, err
}

func (t *tableInfoStore) Replace(tableName string, cfg *TableConfig) error {
	ti, err := t.Get(tableName)
	if err != nil {
		return err
	}

	ti.cfg = cfg
	v, err := encoding.EncodeDocument(ti.ToDocument())
	if err != nil {
		return err
	}

	return t.st.Put([]byte(tableName), v)
}

func (t *tableInfoStore) Get(tableName string) (*tableInfo, error) {
	key := []byte(tableName)
	v, err := t.st.Get(key)
	if err == engine.ErrKeyNotFound {
		return nil, ErrTableNotFound
	}
	if err != nil {
		return nil, err
	}

	var ti tableInfo
	err = ti.ScanDocument(encoding.EncodedDocument(v))
	if err != nil {
		return nil, err
	}

	return &ti, nil
}

func (t *tableInfoStore) Delete(tableName string) error {
	key := []byte(tableName)
	err := t.st.Delete(key)
	if err == engine.ErrKeyNotFound {
		return ErrTableNotFound
	}
	return err
}

func (t *tableInfoStore) ListTables() ([]string, error) {
	it := t.st.NewIterator(engine.IteratorConfig{Reverse: false})

	var names []string
	for it.Seek(nil); it.Valid(); it.Next() {
		k := it.Item().Key()
		names = append(names, string(k))
	}
	return names, it.Close()
}

func generateStoreID() [6]byte {
	var id [6]byte

	binary.BigEndian.PutUint32(id[:], uint32(time.Now().Unix()))
	if _, err := rand.Reader.Read(id[4:]); err != nil {
		panic(fmt.Errorf("cannot generate random number: %v;", err))
	}

	return id
}

// IndexConfig holds the configuration of an index.
type IndexConfig struct {
	// If set to true, values will be associated with at most one key. False by default.
	Unique bool

	IndexName string
	TableName string
	Path      document.ValuePath
}

// ToDocument creates a document from an IndexConfig.
func (i *IndexConfig) ToDocument() document.Document {
	buf := document.NewFieldBuffer()

	buf.Add("unique", document.NewBoolValue(i.Unique))
	buf.Add("indexname", document.NewTextValue(i.IndexName))
	buf.Add("tablename", document.NewTextValue(i.TableName))
	buf.Add("path", document.NewArrayValue(valuePathToArray(i.Path)))
	return buf
}

// ScanDocument implements the document.Scanner interface.
func (i *IndexConfig) ScanDocument(d document.Document) error {
	v, err := d.GetByField("unique")
	if err != nil {
		return err
	}
	i.Unique, err = v.ConvertToBool()
	if err != nil {
		return err
	}

	v, err = d.GetByField("indexname")
	if err != nil {
		return err
	}
	i.IndexName, err = v.ConvertToText()
	if err != nil {
		return err
	}

	v, err = d.GetByField("tablename")
	if err != nil {
		return err
	}
	i.TableName, err = v.ConvertToText()
	if err != nil {
		return err
	}

	v, err = d.GetByField("path")
	if err != nil {
		return err
	}
	i.Path, err = arrayToValuePath(v)
	return err
}

// Index of a table field. Contains information about
// the index configuration and provides methods to manipulate the index.
type Index struct {
	index.Index

	IndexName string
	TableName string
	Path      document.ValuePath
	Unique    bool
}

type indexStore struct {
	st engine.Store
}

func (t *indexStore) Insert(cfg IndexConfig) error {
	key := []byte(cfg.IndexName)
	_, err := t.st.Get(key)
	if err == nil {
		return ErrIndexAlreadyExists
	}
	if err != engine.ErrKeyNotFound {
		return err
	}

	v, err := encoding.EncodeDocument(cfg.ToDocument())
	if err != nil {
		return err
	}

	return t.st.Put(key, v)
}

func (t *indexStore) Get(indexName string) (*IndexConfig, error) {
	key := []byte(indexName)
	v, err := t.st.Get(key)
	if err == engine.ErrKeyNotFound {
		return nil, ErrIndexNotFound
	}
	if err != nil {
		return nil, err
	}

	var idxopts IndexConfig
	err = idxopts.ScanDocument(encoding.EncodedDocument(v))
	if err != nil {
		return nil, err
	}

	return &idxopts, nil
}

func (t *indexStore) Delete(indexName string) error {
	key := []byte(indexName)
	err := t.st.Delete(key)
	if err == engine.ErrKeyNotFound {
		return ErrIndexNotFound
	}
	return err
}

func (t *indexStore) ListAll() ([]*IndexConfig, error) {
	var idxList []*IndexConfig
	it := t.st.NewIterator(engine.IteratorConfig{})

	var buf encoding.EncodedDocument
	var err error
	for it.Seek(nil); it.Valid(); it.Next() {
		item := it.Item()
		var opts IndexConfig
		buf, err = item.ValueCopy(buf)
		if err != nil {
			it.Close()
			return nil, err
		}

		err = opts.ScanDocument(&buf)
		if err != nil {
			it.Close()
			return nil, err
		}

		idxList = append(idxList, &opts)
	}
	err = it.Close()
	if err != nil {
		return nil, err
	}

	return idxList, nil
}

func arrayToValuePath(v document.Value) (document.ValuePath, error) {
	ar, err := v.ConvertToArray()
	if err != nil {
		return nil, err
	}

	var path document.ValuePath

	err = ar.Iterate(func(_ int, value document.Value) error {
		p, err := value.ConvertToText()
		if err != nil {
			return err
		}

		path = append(path, p)
		return nil
	})

	return path, err
}

func valuePathToArray(path document.ValuePath) document.Array {
	abuf := document.NewValueBuffer()
	for _, p := range path {
		abuf = abuf.Append(document.NewTextValue(p))
	}

	return abuf
}
