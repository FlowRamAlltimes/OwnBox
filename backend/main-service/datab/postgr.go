package postgre

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	DB  *pgxpool.Pool
	err error
)

type CustomError struct {
	Op   string
	Msg  string
	Code int
	Err  error
}

func (e *CustomError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s | %v", e.Op, e.Err)
	}
	return fmt.Sprintf("%s", e.Op)
}

func (e *CustomError) Unwrap() error {
	return e.Err
}

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
		return nil, &CustomError{
			Op:   "Creating New Database",
			Code: 500,
			Err:  err,
		}
	}

	_, err = DB.Exec(ctx, Query)
	if err != nil {
		return nil, &CustomError{
			Op:  "Creating new table",
			Err: err,
		}
	}
	return DB, nil
}

func AddData(ctx context.Context, owner, hash string, ts time.Time) error {
	Query := "INSERT INTO cloud(owner, hash, timestamp) VALUES($1, $2, $3);"

	if _, err := DB.Exec(ctx, Query, owner, hash, ts); err != nil {
		return &CustomError{
			Op:   "Appending data",
			Code: 500,
			Msg:  "Unexpected error",
			Err:  err,
		}
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
		return &CustomError{
			Op:   "Taking true owner from table",
			Code: 500,
			Msg:  "Unexpected error",
			Err:  err,
		}
	}

	if TrueOwn != owner {
		return &CustomError{
			Op:   "Comparing owners",
			Code: 403,
			Msg:  "Unexpected error, you can't take this file",
			Err:  nil,
		}
	}

	return nil
}

func RemoveData(ctx context.Context, owner, hash string) error {
	VerificationQuery := "SELECT owner FROM cloud WHERE hash = $1;"
	RemoveQuery := "DELETE FROM cloud WHERE hash = $1;"
	var TrueOwner string
	err := DB.QueryRow(ctx, VerificationQuery, hash).Scan(&TrueOwner)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return &CustomError{
				Op:   "Taking data to remove",
				Code: 404,
				Msg:  "No such file",
				Err:  err,
			}
		} else {
			return &CustomError{
				Op:   "Taking data to remove",
				Code: 500,
				Msg:  "Unexpected error",
				Err:  err,
			}
		}

	}

	if TrueOwner != owner {
		return &CustomError{
			Op:   "Verifying owner",
			Code: 403,
			Msg:  "You can't do it",
			Err:  nil,
		}
	}

	_, err = DB.Exec(ctx, RemoveQuery, hash)
	if err != nil {
		return &CustomError{
			Op:   "Removing data",
			Code: 500,
			Msg:  "Unexpected error",
			Err:  err,
		}
	}

	return nil
}
