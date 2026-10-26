package dbutil

import (
	"context"
	"database/sql"
)

// Database lets a request bind all SQL (including legacy non-Context calls) to
// its deadline without changing the shared connection pool or other requests.
type Database interface {
	Exec(string, ...any) (sql.Result, error)
	Query(string, ...any) (*sql.Rows, error)
	QueryRow(string, ...any) *sql.Row
	Begin() (*sql.Tx, error)
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
	Conn(context.Context) (*sql.Conn, error)
	Close() error
	SetMaxOpenConns(int)
}
type scopedDB struct {
	Database
	ctx context.Context
}

func WithContext(db Database, ctx context.Context) Database { return scopedDB{db, ctx} }
func (d scopedDB) Exec(q string, a ...any) (sql.Result, error) {
	return d.Database.ExecContext(d.ctx, q, a...)
}
func (d scopedDB) Query(q string, a ...any) (*sql.Rows, error) {
	return d.Database.QueryContext(d.ctx, q, a...)
}
func (d scopedDB) QueryRow(q string, a ...any) *sql.Row {
	return d.Database.QueryRowContext(d.ctx, q, a...)
}
func (d scopedDB) Begin() (*sql.Tx, error) { return d.Database.BeginTx(d.ctx, nil) }

// Explicit contexts inside a scoped service cannot extend its request lifetime.
func (d scopedDB) ExecContext(_ context.Context, q string, a ...any) (sql.Result, error) {
	return d.Exec(q, a...)
}
func (d scopedDB) QueryContext(_ context.Context, q string, a ...any) (*sql.Rows, error) {
	return d.Query(q, a...)
}
func (d scopedDB) QueryRowContext(_ context.Context, q string, a ...any) *sql.Row {
	return d.QueryRow(q, a...)
}
func (d scopedDB) BeginTx(_ context.Context, o *sql.TxOptions) (*sql.Tx, error) {
	return d.Database.BeginTx(d.ctx, o)
}
func (d scopedDB) Conn(_ context.Context) (*sql.Conn, error) { return d.Database.Conn(d.ctx) }
