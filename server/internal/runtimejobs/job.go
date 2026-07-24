package runtimejobs

import (
	"context"
	"errors"
)

// Job is a validated, named Lifecycle managed by a Group.
// Implementations are created by NewJob.
type Job interface {
	Lifecycle
	Name() string
	isJob()
}

type job struct {
	name      string
	lifecycle Lifecycle
}

var _ Job = job{}

// NewJob validates and names a lifecycle for runtime orchestration.
func NewJob(name string, lifecycle Lifecycle) (Job, error) {
	if name == "" {
		return nil, errors.New("name must not be empty")
	}
	if lifecycle == nil {
		return nil, errors.New("lifecycle must not be nil")
	}
	return job{name: name, lifecycle: lifecycle}, nil
}

// Name identifies the job within its group.
func (j job) Name() string {
	return j.name
}

// Start delegates activation to the job's lifecycle.
func (j job) Start(ctx context.Context) error {
	return j.lifecycle.Start(ctx)
}

// Stop delegates cleanup to the job's lifecycle.
func (j job) Stop(ctx context.Context) error {
	return j.lifecycle.Stop(ctx)
}

func (job) isJob() {}
