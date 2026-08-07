package deployment

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFleetRedundancyRequiresOneLiveActiveAndOneLivePassive(t *testing.T) {
	for _, test := range []struct {
		name     string
		statuses []fleetHostStatus
		want     bool
	}{
		{name: "active and passive", statuses: []fleetHostStatus{{reachable: true, active: true}, {reachable: true, passive: true}}, want: true},
		{name: "passive unavailable", statuses: []fleetHostStatus{{reachable: true, active: true}, {}}},
		{name: "stale passive", statuses: []fleetHostStatus{{reachable: true, active: true}, {reachable: true}}},
		{name: "two active processes", statuses: []fleetHostStatus{{reachable: true, active: true}, {reachable: true, active: true}}},
		{name: "one host reports both roles", statuses: []fleetHostStatus{{reachable: true, active: true, passive: true}, {reachable: true}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Act
			ready := fleetRedundancyReady(test.statuses)

			// Assert
			require.Equal(t, test.want, ready)
		})
	}
}

func TestSummarizeEtcdMembersRejectsDuplicateAndSplitMembers(t *testing.T) {
	for _, test := range []struct {
		name       string
		identities []etcdMemberIdentity
		want       etcdReadiness
	}{
		{name: "three members", identities: []etcdMemberIdentity{{1, 11}, {1, 12}, {1, 13}}, want: etcdReadiness{quorum: true, redundant: true}},
		{name: "duplicate member", identities: []etcdMemberIdentity{{1, 11}, {1, 12}, {1, 12}}, want: etcdReadiness{quorum: true}},
		{name: "split witness", identities: []etcdMemberIdentity{{1, 11}, {1, 12}, {2, 13}}, want: etcdReadiness{quorum: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Act
			ready := summarizeEtcdMembers(test.identities, 3)

			// Assert
			require.Equal(t, test.want, ready)
		})
	}
}
