package main

import (
	"fmt"
	"time"
)

type config struct {
	Days      int           `help:"Number of days of historical data to generate" default:"10"`
	Interval  time.Duration `help:"Time between data points per device" default:"30s"`
	Devices   int           `help:"Number of synthetic devices to generate" default:"3"`
	BatchSize int           `help:"Rows per INSERT batch" default:"2800"`
	DryRun    bool          `help:"Print stats without inserting"`
	CleanUp   bool          `help:"Delete existing seed data without generating new rows"`
	Outliers  bool          `help:"Inject random outlier spikes for chart testing"`

	DBHost     string `help:"Database host" default:"127.0.0.1" env:"DB_HOST"`
	DBPort     int    `help:"Database port" default:"5432" env:"DB_PORT"`
	DBUser     string `help:"Database user" default:"fleet" env:"DB_USER"`
	DBPassword string `help:"Database password" default:"fleet" env:"DB_PASSWORD"`
	DBName     string `help:"Database name" default:"fleet" env:"DB_NAME"`
}

func (c config) dsn() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
		c.DBUser, c.DBPassword, c.DBHost, c.DBPort, c.DBName)
}

func (c config) validate() error {
	if c.CleanUp {
		return nil
	}
	if c.Days <= 0 {
		return fmt.Errorf("--days must be positive, got %d", c.Days)
	}
	if c.Interval <= 0 {
		return fmt.Errorf("--interval must be positive, got %s", c.Interval)
	}
	if c.Devices <= 0 {
		return fmt.Errorf("--devices must be positive, got %d", c.Devices)
	}
	if c.BatchSize <= 0 {
		return fmt.Errorf("--batch-size must be positive, got %d", c.BatchSize)
	}
	return nil
}
