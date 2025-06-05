package main

import (
	"context"
	"flag"
	"fmt"
	"influx-data-gen/client"
	"influx-data-gen/generator"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/joho/godotenv"
)

var token string

func init() {
	if err := godotenv.Load("../.env"); err != nil {
		log.Printf("warning: could not load .env file: %v", err)
	}
	token = os.Getenv("INFLUXDB3_AUTH_TOKEN")
}

func main() {
	serverURL := flag.String("url", "http://localhost:8181", "InfluxDB server URL")
	token := flag.String("token", token, "InfluxDB authentication token")
	bucket := flag.String("bucket", "fleet", "InfluxDB bucket name")
	duration := flag.Duration("duration", 1*time.Hour, "Time range for data generation (e.g., 1h, 24h)")
	interval := flag.Duration("interval", 1*time.Minute, "Interval between data points (e.g., 1m, 5m)")

	flag.Parse()

	// Validate required flags

	if *token == "" {
		*token = os.Getenv("INFLUXDB_TOKEN")
		if *token == "" {
			log.Fatal("InfluxDB token must be provided via -token flag or INFLUXDB_TOKEN environment variable")
		}
	}

	slog.Info("We have a token", "token", *token)

	influxClient, err := client.New(*serverURL, *token, *bucket)
	if err != nil {
		log.Fatalf("Failed to create InfluxDB client: %v", err)
	}
	defer influxClient.Close()

	slog.Info("InfluxDB client created", "client", influxClient)

	gen := generator.New()

	end := time.Now()
	start := end.Add(-*duration)

	fmt.Printf("Generating metrics from %v to %v with %v interval...\n", start, end, *interval)
	metrics := gen.GenerateMetrics(start, end, *interval)
	fmt.Printf("Generated %d metrics\n", len(metrics))

	ctx := context.Background()
	err = influxClient.WriteMetrics(ctx, metrics)
	if err != nil {
		log.Fatalf("Failed to write metrics: %v", err)
	}

	fmt.Println("Successfully wrote metrics to InfluxDB")
}
