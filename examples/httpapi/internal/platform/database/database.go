package database

import (
	"context"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/palma99/palma-framework/database"
	"github.com/palma99/palma-framework/examples/httpapi/internal/config"
)

// MainDB identifies this application's database in the generated DI graph.
type MainDB struct{ *database.Connection }

//pfw:coconut
func OpenMainDatabase(ctx context.Context, cfg config.Config) (MainDB, func() error, error) {
	db, cleanup, err := database.Open(ctx, database.Config{
		Primary: database.Endpoint{
			Driver: "pgx", DSN: cfg.DB.DSN, ConnectTimeout: cfg.DB.ConnectTimeout,
		},
	})
	return MainDB{Connection: db}, cleanup, err
}
