package main

import (
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"

	"observastack/internal/config"
)

const migrationsSource = "file://migrations"

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: go run ./cmd/migrate <up|down>")
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	migrations, err := migrate.New(migrationsSource, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("create migration runner: %v", err)
	}
	// Close releases both the source and the database connection, reporting each
	// independently. Errors are logged rather than treated as fatal: by this
	// point the migrations have already been applied or rejected, and turning a
	// cleanup failure into a non-zero exit would report a failed run for work
	// that in fact succeeded.
	defer func() {
		if sourceErr, dbErr := migrations.Close(); sourceErr != nil || dbErr != nil {
			log.Printf("close migration runner: source=%v database=%v", sourceErr, dbErr)
		}
	}()

	switch os.Args[1] {
	case "up":
		err = migrations.Up()
	case "down":
		err = migrations.Steps(-1)
	default:
		log.Fatal("usage: go run ./cmd/migrate <up|down>")
	}

	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		log.Fatalf("run migrations: %v", err)
	}

	fmt.Printf("migrations %s complete\n", os.Args[1])
}
