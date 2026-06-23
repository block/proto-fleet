package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEventCategoryValid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		category EventCategory
		want     bool
	}{
		{CategoryAuth, true},
		{CategoryDeviceCommand, true},
		{CategoryFleetManagement, true},
		{CategoryCollection, true},
		{CategoryPool, true},
		{CategorySchedule, true},
		{CategoryCurtailment, true},
		{CategorySystem, true},
		{EventCategory(""), false},
		{EventCategory("unknown"), false},
	}

	for _, tc := range cases {
		t.Run(string(tc.category), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.category.Valid())
		})
	}
}

func TestActorTypeValid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		actor ActorType
		want  bool
	}{
		{ActorUser, true},
		{ActorSystem, true},
		{ActorScheduler, true},
		{ActorCurtailment, true},
		{ActorType(""), false},
		{ActorType("unknown"), false},
	}

	for _, tc := range cases {
		t.Run(string(tc.actor), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.actor.Valid())
		})
	}
}

func TestResultTypeValid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		result ResultType
		want   bool
	}{
		{ResultSuccess, true},
		{ResultFailure, true},
		{ResultType(""), false},
		{ResultType("unknown"), false},
	}

	for _, tc := range cases {
		t.Run(string(tc.result), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.result.Valid())
		})
	}
}

func TestOrgLevelCategories(t *testing.T) {
	t.Parallel()

	got := OrgLevelCategories()
	want := []string{"auth", "system", "pool", "schedule", "curtailment", "device_command"}
	assert.ElementsMatch(t, want, got)
}

func TestOrgLevelCategoriesIsImmutable(t *testing.T) {
	t.Parallel()

	// Mutating the returned slice must not affect later calls — the source
	// is a package-level array and each call returns a fresh copy.
	first := OrgLevelCategories()
	for i := range first {
		first[i] = "tampered"
	}

	assert.ElementsMatch(t,
		[]string{"auth", "system", "pool", "schedule", "curtailment", "device_command"},
		OrgLevelCategories(),
	)
}
