package postgre

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	DB  *pgxpool.Pool
	err error
)

// Initializing Database
func InitDB(ctx context.Context, user, password, host, name string) (*pgxpool.Pool, error) {
	Query := `CREATE TABLE IF NOT EXISTS cloud (
	id SERIAL PRIMARY KEY,
	owner VARCHAR(32) NOT NULL,
	hash VARCHAR(64) NOT NULL,
	timestamp TIMESTAMPTZ NOT NULL
	);`

	connectionLink := fmt.Sprintf("postgres://%s:%s@%s:5432/%s?sslmode=disable", user, password, host, name)

	slog.Debug("DEBUG: Connection link", "link", connectionLink)

	DB, err = pgxpool.New(ctx, connectionLink)
	if err != nil {
		return nil, err
	}

	_, err = DB.Exec(ctx, Query)
	if err != nil {
		return nil, err
	}
	return DB, nil
}

func AddData(ctx context.Context, owner, hash string, ts time.Time) error {
	Query := "INSERT INTO cloud(owner, hash, timestamp) VALUES($1, $2, $3);"

	if _, err := DB.Exec(ctx, Query, owner, hash, ts); err != nil {
		return err
	}

	return nil
}

// Func gets true owner by hash and
// returns error if true owner != logged owner
func DownloadData(ctx context.Context, owner, hash string) error {
	Query := "SELECT owner FROM cloud WHERE hash = $1;"
	var TrueOwn string

	err := DB.QueryRow(ctx, Query, hash).Scan(&TrueOwn)
	if err != nil {
		return err
	}

	if TrueOwn != owner {
		return fmt.Errorf("ERR: Real owner mismatches with logged one in downloading data")
	}

	return nil
}

func RemoveData(ctx context.Context, owner, hash string) error {
	VerificationQuery := "SELECT owner FROM cloud WHERE hash = $1;"
	RemoveQuery := "DELETE FROM cloud WHERE hash = $1;"
	var TrueOwner string
	err := DB.QueryRow(ctx, VerificationQuery, hash).Scan(&TrueOwner)
	if err != nil {
		return err
	}

	if TrueOwner != owner {
		return fmt.Errorf("ERR: Real owner mismatches with logged one in removing data")
	}

	_, err = DB.Exec(ctx, RemoveQuery, hash)
	if err != nil {
		return err
	}

	return nil
}
