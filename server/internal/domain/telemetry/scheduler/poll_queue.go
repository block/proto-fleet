package scheduler

import (
	"time"

	"github.com/block/proto-fleet/server/internal/domain/telemetry/models"
)

type scheduledDevice struct {
	device models.Device
	due    time.Time
	index  int // Negative when the device is outside the queue.
}

type pollQueue []*scheduledDevice

func (q *pollQueue) Len() int { return len(*q) }

func (q *pollQueue) Less(i, j int) bool {
	left, right := (*q)[i], (*q)[j]
	if order := left.due.Compare(right.due); order != 0 {
		return order < 0
	}
	return left.device.LastUpdatedAt.Before(right.device.LastUpdatedAt)
}

func (q *pollQueue) Swap(i, j int) {
	(*q)[i], (*q)[j] = (*q)[j], (*q)[i]
	(*q)[i].index, (*q)[j].index = i, j
}

func (q *pollQueue) Push(value any) {
	entry, ok := value.(*scheduledDevice)
	if !ok {
		panic("poll queue accepts only scheduled devices")
	}
	entry.index = len(*q)
	*q = append(*q, entry)
}

func (q *pollQueue) Pop() any {
	last := len(*q) - 1
	entry := (*q)[last]
	(*q)[last] = nil
	*q = (*q)[:last]
	entry.index = -1
	return entry
}
