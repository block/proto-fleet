package ha

import "time"

const (
	UpdateActiveStopTimeout = 5 * time.Second
	UpdateTakeoverTimeout   = 35 * time.Second
)
