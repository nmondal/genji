package bolt

import (
	"bytes"
	"errors"

	"github.com/asdine/genji/engine"
	"github.com/asdine/genji/index"
	bolt "github.com/etcd-io/bbolt"
)

type Index struct {
	b *bolt.Bucket
}

func NewIndex(b *bolt.Bucket) *Index {
	return &Index{b}
}

func (i *Index) Set(value []byte, rowid []byte) error {
	if len(value) == 0 {
		return errors.New("value cannot be nil")
	}

	buf := make([]byte, 0, len(value)+len(rowid)+1)
	buf = append(buf, value...)
	buf = append(buf, '_')
	buf = append(buf, rowid...)

	err := i.b.Put(buf, rowid)
	if err == bolt.ErrTxNotWritable {
		return engine.ErrTransactionReadOnly
	}

	return err
}

func (i *Index) Cursor() index.Cursor {
	return &Cursor{
		b: i.b,
		c: i.b.Cursor(),
	}
}

type Cursor struct {
	b   *bolt.Bucket
	c   *bolt.Cursor
	val []byte
}

func (c *Cursor) First() ([]byte, []byte) {
	value, rowid := c.c.First()
	if value == nil {
		return nil, nil
	}

	return value[:bytes.LastIndexByte(value, '_')], rowid
}

func (c *Cursor) Last() ([]byte, []byte) {
	value, rowid := c.c.Last()
	if value == nil {
		return nil, nil
	}

	return value[:bytes.LastIndexByte(value, '_')], rowid
}

func (c *Cursor) Next() ([]byte, []byte) {
	value, rowid := c.c.Next()
	if value == nil {
		c.c.Last()
		return nil, nil
	}

	return value[:bytes.LastIndexByte(value, '_')], rowid
}

func (c *Cursor) Prev() ([]byte, []byte) {
	value, rowid := c.c.Prev()
	if value == nil {
		c.c.First()
		return nil, nil
	}

	return value[:bytes.LastIndexByte(value, '_')], rowid
}

func (c *Cursor) Seek(seek []byte) ([]byte, []byte) {
	value, rowid := c.c.Seek(seek)
	if value == nil {
		return nil, nil
	}

	return value[:bytes.LastIndexByte(value, '_')], rowid
}
