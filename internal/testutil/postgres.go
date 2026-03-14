package testutil

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"math/big"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

const mainDSN = "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable"

var mainPool = sync.OnceValue(func() *pgxpool.Pool {
	pool, err := pgxpool.New(context.Background(), mainDSN)
	if err != nil {
		panic(fmt.Sprintf("testutil: cannot connect to postgres: %v", err))
	}
	return pool
})

// NewPostgres creates an isolated test database, runs the given migration SQL,
// and returns a pool + cleanup function. The database is dropped on cleanup.
func NewPostgres(t *testing.T, migrationSQL string) *pgxpool.Pool {
	t.Helper()
	dbName := randomName()

	if _, err := mainPool().Exec(t.Context(), fmt.Sprintf("CREATE DATABASE %s", dbName)); err != nil {
		t.Fatalf("testutil: create database %s: %v", dbName, err)
	}

	dsn := fmt.Sprintf("postgres://postgres:postgres@localhost:5432/%s?sslmode=disable", dbName)
	pool, err := pgxpool.New(t.Context(), dsn)
	if err != nil {
		t.Fatalf("testutil: connect to %s: %v", dbName, err)
	}

	if migrationSQL != "" {
		if _, err := pool.Exec(t.Context(), migrationSQL); err != nil {
			pool.Close()
			t.Fatalf("testutil: migration on %s: %v", dbName, err)
		}
	}

	t.Cleanup(func() {
		pool.Close()
		teardown(dbName)
	})

	return pool
}

func teardown(dbName string) {
	ctx := context.Background()
	p := mainPool()

	// Kill all connections
	_, err := p.Exec(ctx, fmt.Sprintf(
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s' AND pid <> pg_backend_pid()", dbName))
	if err != nil {
		log.Printf("testutil: terminate connections to %s: %v", dbName, err)
	}

	if _, err := p.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName)); err != nil {
		log.Printf("testutil: drop database %s: %v", dbName, err)
	}
}

func randomName() string {
	name := "test_"
	for range 10 {
		n, _ := rand.Int(rand.Reader, big.NewInt(26))
		name += string(rune('a' + n.Int64()))
	}
	return name
}
