package postgres

import (
	"embed"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/*/*.sql
var migrations embed.FS

// Migrate is explicit administration, never an automatic side effect of API startup.
func Migrate(databaseURL, scope string) error {
	if scope != "app" && scope != "supplier" {
		return errors.New("migration scope must be app or supplier")
	}
	u, err := url.Parse(databaseURL)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return errors.New("invalid DATABASE_URL")
	}
	u.Scheme = "pgx5"
	q := u.Query()
	q.Set("connect_timeout", "5")
	q.Set("x-statement-timeout", "10000")
	u.RawQuery = q.Encode()
	source, err := iofs.New(migrations, "migrations/"+scope)
	if err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", source, u.String())
	if err != nil {
		return errors.Join(fmt.Errorf("initialize migrations: %w", err), source.Close())
	}
	m.LockTimeout = 10 * time.Second
	err = m.Up()
	if errors.Is(err, migrate.ErrNoChange) {
		err = nil
	}
	sourceErr, databaseErr := m.Close()
	return errors.Join(err, sourceErr, databaseErr)
}
