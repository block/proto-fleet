package generator

import (
	"influx-data-gen/models"
	"math/rand"
	"time"
)

type Generator struct {
	statusWeights map[models.Status]float64
	rand          *rand.Rand
}

func New() *Generator {
	return &Generator{
		statusWeights: map[models.Status]float64{
			models.StatusOK:       0.7,
			models.StatusWarn:     0.2,
			models.StatusCritical: 0.1,
		},
		rand: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// WithStatusWeights allows customizing the probability distribution of statuses
func (g *Generator) WithStatusWeights(weights map[models.Status]float64) *Generator {
	g.statusWeights = weights
	return g
}

func (g *Generator) generateStatus() models.Status {
	r := g.rand.Float64()
	var cumulative float64
	for status, weight := range g.statusWeights {
		cumulative += weight
		if r <= cumulative {
			return status
		}
	}
	return models.StatusOK
}

func randomChoice[T any](r *rand.Rand, items []T) T {
	return items[r.Intn(len(items))]
}

func (g *Generator) GenerateMetric(timestamp time.Time) models.CPUMetric {
	return models.CPUMetric{
		Timestamp:    timestamp,
		Host:         randomChoice(g.rand, models.AllHosts()),
		Region:       randomChoice(g.rand, models.AllRegions()),
		Application:  randomChoice(g.rand, models.AllApplications()),
		Value:        int64(g.rand.Intn(100) + 1),
		UsagePercent: float64(g.rand.Intn(1000)) / 10.0, // 0.0 to 100.0
		Status:       g.generateStatus(),
	}
}

// GenerateMetrics creates multiple CPU metrics within a time range
func (g *Generator) GenerateMetrics(start, end time.Time, interval time.Duration) []models.CPUMetric {
	var metrics []models.CPUMetric

	for t := start; t.Before(end); t = t.Add(interval) {
		// Generate 3-5 metrics per interval
		numMetrics := g.rand.Intn(3) + 3
		for i := 0; i < numMetrics; i++ {
			metrics = append(metrics, g.GenerateMetric(t))
		}
	}

	return metrics
}
