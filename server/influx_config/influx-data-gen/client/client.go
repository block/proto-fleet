package client

import (
	"context"
	"fmt"
	"influx-data-gen/models"

	"github.com/InfluxCommunity/influxdb3-go/v2/influxdb3"
)

type Client struct {
	client *influxdb3.Client
	bucket string
}

func New(serverURL, token, bucket string) (*Client, error) {
	client, err := influxdb3.New(influxdb3.ClientConfig{
		Host:     serverURL,
		Token:    token,
		Database: bucket,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create InfluxDB client: %w", err)
	}

	return &Client{
		client: client,
		bucket: bucket,
	}, nil
}

func (c *Client) Close() {
	c.client.Close()
}

// WriteMetrics writes multiple CPU metrics to InfluxDB
func (c *Client) WriteMetrics(ctx context.Context, metrics []models.CPUMetric) error {
	points := make([]*influxdb3.Point, 0, len(metrics))
	for _, metric := range metrics {
		points = append(points, metric.ToPoint())
	}

	for i := 0; i < len(points); i += 10000 {
		err := c.client.WritePoints(
			ctx,
			points[i:min(i+10000, len(points))],
		)
		if err != nil {
			return fmt.Errorf("failed to write metrics: %w", err)
		}
	}

	return nil
}
