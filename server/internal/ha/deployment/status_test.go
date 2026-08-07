package deployment

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHostProbeDSNUsesHostCAAndStatementCache(t *testing.T) {
	// Act
	dsn, err := hostProbeDSN(
		"postgresql://fleet:secret@10.0.0.1:5432,10.0.0.2:5432/fleet?sslmode=verify-full&sslrootcert=/run/proto-fleet-ha/service-ca.crt&target_session_attrs=read-write",
		"/etc/proto-fleet/ha",
	)

	// Assert
	require.NoError(t, err)
	parsed, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Equal(t, "/etc/proto-fleet/ha/service-ca.crt", parsed.Query().Get("sslrootcert"))
	require.Equal(t, "cache_statement", parsed.Query().Get("default_query_exec_mode"))
	require.Equal(t, "read-write", parsed.Query().Get("target_session_attrs"))
}

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

func TestMatchingFleetVersions(t *testing.T) {
	for _, test := range []struct {
		name     string
		statuses []fleetHostStatus
		want     bool
	}{
		{name: "same version", statuses: []fleetHostStatus{{version: "v1.2.0"}, {version: "v1.2.0"}}, want: true},
		{name: "different versions", statuses: []fleetHostStatus{{version: "v1.2.0"}, {version: "v1.1.0"}}},
		{name: "missing version", statuses: []fleetHostStatus{{version: "v1.2.0"}, {}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Act
			matches := matchingFleetVersions(test.statuses)

			// Assert
			require.Equal(t, test.want, matches)
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
