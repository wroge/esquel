package esquel

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

type Querier interface {
	QueryContext(ctx context.Context, sql string, args ...any) (*sql.Rows, error)
}

type Executor interface {
	ExecContext(ctx context.Context, sql string, args ...any) (sql.Result, error)
}

type Placeholder interface {
	ReplacePlaceholders(sql string) (string, error)
}

const (
	Question = StaticPlaceholder("?")
	Dollar   = PositionalPlaceholder("$")
	Colon    = PositionalPlaceholder(":")
	AtP      = PositionalPlaceholder("@p")
)

type StaticPlaceholder string

func (sp StaticPlaceholder) ReplacePlaceholders(sql string) (string, error) {
	if sp == "?" {
		return sql, nil
	}

	var (
		builder  strings.Builder
		argIndex int
	)

	for {
		index := strings.IndexRune(sql, '?')
		if index < 0 {
			builder.WriteString(sql)

			break
		}

		if index < len(sql)-1 && sql[index+1] == '?' {
			builder.WriteString(sql[:index+1])
			sql = sql[index+2:]

			continue
		}

		argIndex++

		builder.WriteString(sql[:index] + string(sp))
		sql = sql[index+1:]
	}

	return builder.String(), nil
}

type PositionalPlaceholder string

func (pp PositionalPlaceholder) ReplacePlaceholders(sql string) (string, error) {
	var (
		builder  strings.Builder
		argIndex int
	)

	for {
		index := strings.IndexRune(sql, '?')
		if index < 0 {
			builder.WriteString(sql)

			break
		}

		if index < len(sql)-1 && sql[index+1] == '?' {
			builder.WriteString(sql[:index+1])
			sql = sql[index+2:]

			continue
		}

		argIndex++

		builder.WriteString(sql[:index] + string(pp) + strconv.Itoa(argIndex))
		sql = sql[index+1:]
	}

	return builder.String(), nil
}

type Statement[P any] interface {
	ToSQL(param P) (string, []any, error)
}

func Template[P any](sql string, args ...Statement[P]) Statement[P] {
	return templateStatement[P]{
		sql:  sql,
		args: args,
	}
}

type templateStatement[P any] struct {
	sql  string
	args []Statement[P]
}

func (t templateStatement[P]) ToSQL(param P) (string, []any, error) {
	var (
		builder   strings.Builder
		arguments = make([]any, 0, len(t.args))
		exprIndex int
	)

	for {
		index := strings.IndexRune(t.sql, '?')
		if index < 0 {
			builder.WriteString(t.sql)

			break
		}

		if index < len(t.sql)-1 && t.sql[index+1] == '?' {
			builder.WriteString(t.sql[:index+2])
			t.sql = t.sql[index+2:]

			continue
		}

		builder.WriteString(t.sql[:index])
		t.sql = t.sql[index+1:]

		if exprIndex >= len(t.args) || t.args[exprIndex] == nil {
			builder.WriteByte('?')
			arguments = append(arguments, param)

			continue
		} else {
			sql, args, err := t.args[exprIndex].ToSQL(param)
			if err != nil {
				return "", nil, err
			}

			if sql == "" {
				if len(t.sql) > 0 && t.sql[0] == ' ' {
					t.sql = t.sql[1:]
				}

				continue
			}

			builder.WriteString(sql)
			arguments = append(arguments, args...)
		}
		exprIndex++
	}

	for ; exprIndex < len(t.args); exprIndex++ {
		if t.args[exprIndex] == nil {
			continue
		}

		sql, args, err := t.args[exprIndex].ToSQL(param)
		if err != nil {
			return "", nil, err
		}

		if sql == "" {
			continue
		}

		if builder.Len() > 0 {
			builder.WriteByte(' ')
		}

		builder.WriteString(sql)
		arguments = append(arguments, args...)
	}

	return strings.TrimSpace(builder.String()), arguments, nil
}

func Map[V any, P any](m func(P) V, stmt Statement[V]) Statement[P] {
	return mapStatement[V, P]{
		mapper: m,
		stmt:   stmt,
	}
}

type mapStatement[V any, P any] struct {
	mapper func(param P) V
	stmt   Statement[V]
}

func (m mapStatement[V, P]) ToSQL(param P) (string, []any, error) {
	v := m.mapper(param)

	if m.stmt == nil {
		return "?", []any{v}, nil
	}

	return m.stmt.ToSQL(v)
}

func Self[P any](f func(self Statement[P], param P) (string, []any, error)) Statement[P] {
	return selfStatement[P](f)
}

type selfStatement[P any] func(self Statement[P], param P) (string, []any, error)

func (s selfStatement[P]) ToSQL(param P) (string, []any, error) {
	return s(s, param)
}

func Func[P any](f func(param P) (string, []any, error)) Statement[P] {
	return paramStatement[P](f)
}

type paramStatement[P any] func(param P) (string, []any, error)

func (f paramStatement[P]) ToSQL(param P) (string, []any, error) {
	return f(param)
}

func Prefix[P any](prefix string, statement Statement[P]) Statement[P] {
	return prefixStatement[P]{
		prefix: prefix,
		stmt:   statement,
	}
}

type prefixStatement[P any] struct {
	prefix string
	stmt   Statement[P]
}

func (f prefixStatement[P]) ToSQL(param P) (string, []any, error) {
	if f.stmt == nil {
		return "", nil, nil
	}

	sql, args, err := f.stmt.ToSQL(param)
	if err != nil {
		return "", nil, err
	}

	if sql == "" {
		return "", nil, nil
	}

	return f.prefix + " " + sql, args, nil
}

func Join[P any](sep string, stmts ...Statement[P]) Statement[P] {
	return joinStatement[P]{
		sep:   sep,
		stmts: stmts,
	}
}

type joinStatement[P any] struct {
	sep   string
	stmts []Statement[P]
}

func (js joinStatement[P]) ToSQL(param P) (string, []any, error) {
	var (
		b         strings.Builder
		arguments = make([]any, 0, len(js.stmts))
	)

	for _, s := range js.stmts {
		if s == nil {
			continue
		}

		sql, args, err := s.ToSQL(param)
		if err != nil {
			return "", nil, err
		}

		if sql == "" {
			continue
		}

		if b.Len() > 0 {
			b.WriteString(js.sep)
		}

		b.WriteString(sql)
		arguments = append(arguments, args...)
	}

	return b.String(), arguments, nil
}

func List[P any](sep string, statement Statement[P]) Statement[[]P] {
	return listStatement[P]{
		sep:  sep,
		stmt: statement,
	}
}

type listStatement[P any] struct {
	sep  string
	stmt Statement[P]
}

func (jp listStatement[P]) ToSQL(param []P) (string, []any, error) {
	var (
		b         strings.Builder
		arguments = make([]any, 0, len(param))
	)

	for _, p := range param {
		if jp.stmt == nil {
			if b.Len() > 0 {
				b.WriteString(jp.sep)
			}

			b.WriteString("?")
			arguments = append(arguments, p)

			continue
		}

		sql, args, err := jp.stmt.ToSQL(p)
		if err != nil {
			return "", nil, err
		}

		if sql == "" {
			continue
		}

		if b.Len() > 0 {
			b.WriteString(jp.sep)
		}

		b.WriteString(sql)
		arguments = append(arguments, args...)
	}

	return b.String(), arguments, nil
}

type Scanner[T any] func() (any, func(*T) error)

func Scan[V, T any](f func(*T, V)) Scanner[T] {
	return func() (any, func(*T) error) {
		var v V

		return &v, func(t *T) error {
			f(t, v)

			return nil
		}
	}
}

func ScanNull[V, T any](def V, f func(*T, V)) Scanner[T] {
	return func() (any, func(*T) error) {
		var v *V

		return &v, func(t *T) error {
			if v == nil {
				f(t, def)
			} else {
				f(t, *v)
			}

			return nil
		}
	}
}

func ScanJSON[V, T any](f func(*T, V)) Scanner[T] {
	return func() (any, func(*T) error) {
		var b []byte

		return &b, func(t *T) error {
			var v V

			if err := json.Unmarshal(b, &v); err != nil {
				return err
			}

			f(t, v)

			return nil
		}
	}
}

type Query[T any, P any] struct {
	Placeholder Placeholder
	Statement   Statement[P]
	Columns     map[string]Scanner[T]
}

func (q Query[T, P]) Rows(ctx context.Context, querier Querier, param P) (*Rows[T], error) {
	sql, args, err := q.Statement.ToSQL(param)
	if err != nil {
		return nil, err
	}

	if q.Placeholder != nil {
		sql, err = q.Placeholder.ReplacePlaceholders(sql)
		if err != nil {
			return nil, err
		}
	}

	rows, err := querier.QueryContext(ctx, sql, args...)
	if err != nil {
		return nil, err
	}

	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var dest = make([]any, len(columns))

	if len(q.Columns) == 0 {
		var v T

		dest[0] = &v

		for i := range dest {
			if i > 0 {
				dest[i] = new(any)
			}
		}

		return &Rows[T]{
			Rows: rows,
			Dest: dest,
			Map: func(t *T) error {
				*t = v

				return nil
			},
		}, nil
	}

	var mappers = make([]func(*T) error, len(columns))

	for i, c := range columns {
		if s, ok := q.Columns[c]; ok && s != nil {
			dest[i], mappers[i] = s()
		} else {
			dest[i] = new(any)
		}
	}

	return &Rows[T]{
		Rows: rows,
		Dest: dest,
		Map: func(t *T) error {
			for _, m := range mappers {
				if m != nil {
					if err := m(t); err != nil {
						return err
					}
				}
			}

			return nil
		},
	}, nil
}

func (q Query[T, P]) All(ctx context.Context, querier Querier, param P) ([]T, error) {
	rows, err := q.Rows(ctx, querier, param)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	var (
		list  []T
		index int
	)

	for rows.Next() {
		list = append(list, *new(T))

		if err = rows.Scan(&list[index]); err != nil {
			return nil, err
		}

		index++
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return list, rows.Close()
}

func (q Query[T, P]) First(ctx context.Context, querier Querier, param P) (T, error) {
	var t T

	rows, err := q.Rows(ctx, querier, param)
	if err != nil {
		return t, err
	}

	defer rows.Close()

	if !rows.Next() {
		return t, sql.ErrNoRows
	}

	if err = rows.Scan(&t); err != nil {
		return t, err
	}

	if err = rows.Err(); err != nil {
		return t, err
	}

	return t, rows.Close()
}

var ErrTooManyRows = errors.New("sql: too many rows in result set")

func (q Query[T, P]) One(ctx context.Context, querier Querier, param P) (T, error) {
	var t T

	rows, err := q.Rows(ctx, querier, param)
	if err != nil {
		return t, err
	}

	defer rows.Close()

	if !rows.Next() {
		return t, sql.ErrNoRows
	}

	if err = rows.Scan(&t); err != nil {
		return t, err
	}

	if rows.Next() {
		return t, ErrTooManyRows
	}

	if err = rows.Err(); err != nil {
		return t, err
	}

	return t, rows.Close()
}

type Rows[T any] struct {
	Rows *sql.Rows
	Dest []any
	Map  func(*T) error
}

func (r *Rows[T]) Next() bool {
	return r.Rows.Next()
}

func (r *Rows[T]) Scan(t *T) error {
	if err := r.Rows.Scan(r.Dest...); err != nil {
		return err
	}
	return r.Map(t)
}

func (r *Rows[T]) Value() (T, error) {
	var t T

	return t, r.Scan(&t)
}

func (r *Rows[T]) Err() error {
	return r.Rows.Err()
}

func (r *Rows[T]) Close() error {
	return r.Rows.Close()
}

type Exec[P any] struct {
	Placeholder Placeholder
	Statement   Statement[P]
}

func (es Exec[P]) Result(ctx context.Context, executor Executor, param P) (sql.Result, error) {
	sql, args, err := es.Statement.ToSQL(param)
	if err != nil {
		return nil, err
	}

	if es.Placeholder != nil {
		sql, err = es.Placeholder.ReplacePlaceholders(sql)
		if err != nil {
			return nil, err
		}
	}

	return executor.ExecContext(ctx, sql, args...)
}
