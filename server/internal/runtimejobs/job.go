package runtimejobs

import (
	"context"
	"errors"
)

// Job is a validated, named Lifecycle managed by a Group.
type Job struct {
	name      string
	lifecycle Lifecycle
}

var _ Lifecycle = Job{}

// NewJob validates and names a lifecycle for runtime orchestration.
func NewJob(name string, lifecycle Lifecycle) (Job, error) {
	job := Job{name: name, lifecycle: lifecycle}
	if err := job.validate(); err != nil {
		return Job{}, err
	}
	return job, nil
}

// Name identifies the job within its group.
func (j Job) Name() string {
	return j.name
}

// Start delegates activation to the job's lifecycle.
func (j Job) Start(ctx context.Context) error {
	return j.lifecycle.Start(ctx)
}

// Stop delegates cleanup to the job's lifecycle.
func (j Job) Stop(ctx context.Context) error {
	return j.lifecycle.Stop(ctx)
}

func (j Job) validate() error {
	if j.name == "" {
		return errors.New("name must not be empty")
	}
	if j.lifecycle == nil {
		return errors.New("lifecycle must not be nil")
	}
	return nil
}
