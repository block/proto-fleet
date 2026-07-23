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
		{ActorMinerChannel, true},
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

func TestResolveSiteScope(t *testing.T) {
	t.Parallel()

	siteA := int64(10)
	siteB := int64(20)
	ptr := func(v int64) *int64 { return &v }

	cases := []struct {
		name string
		in   []*int64
		want SiteScope
	}{
		{
			name: "empty set → org-scoped, not multi-site",
			in:   nil,
			want: SiteScope{},
		},
		{
			name: "single device at site A → stamp A (scalar fast path)",
			in:   []*int64{ptr(siteA)},
			want: SiteScope{SiteID: &siteA},
		},
		{
			name: "multiple devices all at site A → stamp A",
			in:   []*int64{ptr(siteA), ptr(siteA)},
			want: SiteScope{SiteID: &siteA},
		},
		{
			name: "every device site-less → single unassigned slot, not multi-site",
			in:   []*int64{nil, nil},
			want: SiteScope{},
		},
		{
			name: "two distinct sites → multi-site membership, no unassigned",
			in:   []*int64{ptr(siteA), ptr(siteB)},
			want: SiteScope{MultiSite: true, MemberSiteIDs: []int64{siteA, siteB}},
		},
		{
			name: "duplicate sites collapse in membership",
			in:   []*int64{ptr(siteA), ptr(siteB), ptr(siteA)},
			want: SiteScope{MultiSite: true, MemberSiteIDs: []int64{siteA, siteB}},
		},
		{
			name: "one site + site-less → 2 slots → multi-site, touches unassigned",
			in:   []*int64{ptr(siteA), nil},
			want: SiteScope{MultiSite: true, MemberSiteIDs: []int64{siteA}, TouchesUnassigned: true},
		},
		{
			name: "two sites + site-less → multi-site, touches unassigned",
			in:   []*int64{ptr(siteA), ptr(siteB), nil},
			want: SiteScope{MultiSite: true, MemberSiteIDs: []int64{siteA, siteB}, TouchesUnassigned: true},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ResolveSiteScope(tc.in)
			assert.Equal(t, tc.want.SiteID, got.SiteID)
			assert.Equal(t, tc.want.MultiSite, got.MultiSite)
			assert.Equal(t, tc.want.TouchesUnassigned, got.TouchesUnassigned)
			assert.ElementsMatch(t, tc.want.MemberSiteIDs, got.MemberSiteIDs)
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
