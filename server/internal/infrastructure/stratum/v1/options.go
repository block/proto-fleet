package stratum

import (
	"time"
)

type Options struct {
	requestTimeout time.Duration
}

type Option func(*Options)

func WithTimeout(duration time.Duration) Option {
	return func(o *Options) {
		o.requestTimeout = duration
	}
}
