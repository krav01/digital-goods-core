package main

import (
	"flag"
	"log/slog"
	"os"

	"github.com/krav01/digital-goods-core/internal/postgres"
)

func main() {
	scope := flag.String("scope", "app", "migration scope: app or supplier")
	flag.Parse()
	if err := postgres.Migrate(os.Getenv("DATABASE_URL"), *scope); err != nil {
		slog.Error("migration failed", "error", err)
		os.Exit(1)
	}
	slog.Info("migrations applied", "scope", *scope)
}
