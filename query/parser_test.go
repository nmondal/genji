package query

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParserExpr(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		expected Expr
	}{
		{"=", "age = 10", Eq(Field("age"), Int64Value(10))},
		{"AND", "age = 10 AND age <= 11",
			And(
				Eq(Field("age"), Int64Value(10)),
				Lte(Field("age"), Int64Value(11)),
			)},
		{"OR", "age = 10 OR age = 11",
			Or(
				Eq(Field("age"), Int64Value(10)),
				Eq(Field("age"), Int64Value(11)),
			)},
		{"AND then OR", "age >= 10 AND age > $age OR age < 10.4",
			Or(
				And(
					Gte(Field("age"), Int64Value(10)),
					Gt(Field("age"), NamedParam("age")),
				),
				Lt(Field("age"), Float64Value(10.4)),
			)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ex, err := NewParser(strings.NewReader(test.s)).ParseExpr()
			require.NoError(t, err)
			require.EqualValues(t, test.expected, ex)
		})
	}
}

func TestParserParams(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		expected Expr
		errored  bool
	}{
		{"one positional", "age = ?", Eq(Field("age"), PositionalParam(1)), false},
		{"multiple positional", "age = ? AND age <= ?",
			And(
				Eq(Field("age"), PositionalParam(1)),
				Lte(Field("age"), PositionalParam(2)),
			), false},
		{"one named", "age = $age", Eq(Field("age"), NamedParam("age")), false},
		{"multiple named", "age = $foo OR age = $bar",
			Or(
				Eq(Field("age"), NamedParam("foo")),
				Eq(Field("age"), NamedParam("bar")),
			), false},
		{"mixed", "age >= ? AND age > $foo OR age < ?", nil, true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ex, err := NewParser(strings.NewReader(test.s)).ParseExpr()
			if test.errored {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.EqualValues(t, test.expected, ex)
			}
		})
	}
}

func TestParserMultiStatement(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		expected []Statement
	}{
		{"OnlyCommas", ";;;", nil},
		{"TrailingComma", "SELECT * FROM foo;;;DELETE FROM foo;", []Statement{
			Select().From(Table("foo")),
			Delete().From(Table("foo")),
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			q, err := ParseQuery(test.s)
			require.NoError(t, err)
			require.EqualValues(t, test.expected, q.statements)
		})
	}
}

func TestParserSelect(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		expected Statement
	}{
		{"NoCond", "SELECT * FROM test", Select().From(Table("test"))},
		{"WithFields", "SELECT a, b FROM test", Select(Field("a"), Field("b")).From(Table("test"))},
		{"WithCond", "SELECT * FROM test WHERE age = 10", Select().From(Table("test")).Where(Eq(Field("age"), Int64Value(10)))},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			q, err := ParseQuery(test.s)
			require.NoError(t, err)
			require.Len(t, q.statements, 1)
			require.EqualValues(t, test.expected, q.statements[0])
		})
	}
}

func TestParserDelete(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		expected Statement
	}{
		{"NoCond", "DELETE FROM test", Delete().From(Table("test"))},
		{"WithCond", "DELETE FROM test WHERE age = 10", Delete().From(Table("test")).Where(Eq(Field("age"), Int64Value(10)))},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			q, err := ParseQuery(test.s)
			require.NoError(t, err)
			require.Len(t, q.statements, 1)
			require.EqualValues(t, test.expected, q.statements[0])
		})
	}
}

func TestParserUdpate(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		expected Statement
		errored  bool
	}{
		{"No cond", "UPDATE test SET a = 1", Update(Table("test")).Set("a", Int64Value(1)), false},
		{"With cond", "UPDATE test SET a = 1, b = 2 WHERE age = 10", Update(Table("test")).Set("a", Int64Value(1)).Set("b", Int64Value(2)).Where(Eq(Field("age"), Int64Value(10))), false},
		{"Trailing comma", "UPDATE test SET a = 1, WHERE age = 10", nil, true},
		{"No SET", "UPDATE test WHERE age = 10", nil, true},
		{"No pair", "UPDATE test SET WHERE age = 10", nil, true},
		{"Field only", "UPDATE test SET a WHERE age = 10", nil, true},
		{"No value", "UPDATE test SET a = WHERE age = 10", nil, true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			q, err := ParseQuery(test.s)
			if test.errored {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Len(t, q.statements, 1)
			require.EqualValues(t, test.expected, q.statements[0])
		})
	}
}

func TestParserInsert(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		expected Statement
		errored  bool
	}{
		{"Values / No columns", "INSERT INTO test VALUES ('a', 'b', 'c')", Insert().Into(Table("test")).Values(StringValue("a"), StringValue("b"), StringValue("c")), false},
		{"Values / With columns", "INSERT INTO test (a, b) VALUES ('c', 'd', 'e')",
			Insert().Into(Table("test")).
				Fields("a", "b").
				Values(StringValue("c"), StringValue("d"), StringValue("e")), false},
		{"Values / Multple", "INSERT INTO test (a, b) VALUES ('c', 'd'), ('e', 'f')",
			Insert().Into(Table("test")).
				Fields("a", "b").
				Values(StringValue("c"), StringValue("d")).
				Values(StringValue("e"), StringValue("f")), false},
		{"Records", "INSERT INTO test RECORDS (a: 'a', b: 2.3, c: 1 = 1)",
			Insert().Into(Table("test")).
				pairs(kvPair{"a", StringValue("a")}, kvPair{"b", Float64Value(2.3)}, kvPair{"c", Eq(Int64Value(1), Int64Value(1))}), false},
		{"Records / Multiple", "INSERT INTO test RECORDS (a: 'a', b: 2.3, c: 1 = 1), (a: 1, d: true)",
			Insert().Into(Table("test")).
				pairs(kvPair{"a", StringValue("a")}, kvPair{"b", Float64Value(2.3)}, kvPair{"c", Eq(Int64Value(1), Int64Value(1))}).
				pairs(kvPair{"a", Int64Value(1)}, kvPair{"d", BoolValue(true)}), false},
		{"Records / Positional Param", "INSERT INTO test RECORDS ?, ?",
			func() Statement {
				st := Insert().Into(Table("test"))
				st.records = append(st.records, PositionalParam(1), PositionalParam(2))
				return st
			}(), false},
		{"Records / Named Param", "INSERT INTO test RECORDS $foo, $bar",
			func() Statement {
				st := Insert().Into(Table("test"))
				st.records = append(st.records, NamedParam("foo"), NamedParam("bar"))
				return st
			}(), false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			q, err := ParseQuery(test.s)
			if test.errored {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Len(t, q.statements, 1)
			require.EqualValues(t, test.expected, q.statements[0])
		})
	}
}

func TestParserCreateTable(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		expected Statement
		errored  bool
	}{
		{"Basic", "CREATE TABLE test", CreateTable("test"), false},
		{"If not exists", "CREATE TABLE test IF NOT EXISTS", CreateTable("test").IfNotExists(), false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			q, err := ParseQuery(test.s)
			if test.errored {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Len(t, q.statements, 1)
			require.EqualValues(t, test.expected, q.statements[0])
		})
	}
}
