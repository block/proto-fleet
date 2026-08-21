package db

import (
	"database/sql"
	"sync"
)

var poolResetRegistry sync.Map

func RegisterIdleConnectionPoolReset(conn *sql.DB, maxIdleConns int) {
	registerPoolReset(conn, func() {
		conn.SetMaxIdleConns(0)
		conn.SetMaxIdleConns(maxIdleConns)
	})
}

func registerPoolReset(conn *sql.DB, reset func()) {
	if conn == nil || reset == nil {
		return
	}
	poolResetRegistry.Store(conn, reset)
}

func poolResetFor(conn *sql.DB) func() {
	if conn == nil {
		return nil
	}
	reset, ok := poolResetRegistry.Load(conn)
	if !ok {
		return nil
	}
	resetFn, _ := reset.(func())
	return resetFn
}
