package deployment

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/block/proto-fleet/server/internal/ha"
)

// EtcdMemberStatus separates allocated backend space from live data and retained
// history. Their difference is reusable space, not evidence of deleted keys.
type EtcdMemberStatus struct {
	Endpoint    string `json:"endpoint"`
	Available   bool   `json:"available"`
	DBSize      int64  `json:"db_size_bytes"`
	DBSizeInUse int64  `json:"db_size_in_use_bytes"`
	DBSizeQuota int64  `json:"db_size_quota_bytes"`
	Warning     bool   `json:"warning"`
}

type EtcdReport struct {
	Healthy bool               `json:"healthy"`
	Members []EtcdMemberStatus `json:"members"`
}

// EtcdStatus is usable on all three hosts without Fleet or PostgreSQL running.
// It uses the existing read-only Fleet observer credential, never the root user.
func EtcdStatus(ctx context.Context, envPath string) (EtcdReport, error) {
	config, err := loadNodeConfig(envPath)
	if err != nil {
		return EtcdReport{}, err
	}
	if err := validateNodeConfig(config); err != nil {
		return EtcdReport{}, err
	}
	tlsConfig, err := ha.LoadServiceTLS(filepath.Join(config.SecretsDir, "service-ca.crt"))
	if err != nil {
		return EtcdReport{}, err
	}
	password, err := readPassword(filepath.Join(config.SecretsDir, fleetEtcdPasswordFile))
	if err != nil {
		return EtcdReport{}, fmt.Errorf("read Fleet etcd password: %w", err)
	}
	status := probeEtcdMembers(ctx, clientv3.Config{
		Endpoints: []string{"https://" + config.DatabaseAIP + ":2379", "https://" + config.DatabaseBIP + ":2379", "https://" + config.WitnessIP + ":2379"},
		Username:  "fleet-observer", Password: password, TLS: tlsConfig, DialTimeout: 2 * time.Second,
	})
	return EtcdReport{Healthy: status.redundant && status.spaceHealthy, Members: status.members}, nil
}

func etcdMemberSpace(endpoint string, status *clientv3.StatusResponse) EtcdMemberStatus {
	return EtcdMemberStatus{
		Endpoint: endpoint, Available: true, DBSize: status.DbSize, DBSizeInUse: status.DbSizeInUse, DBSizeQuota: status.DbSizeQuota,
		Warning: status.DbSizeQuota <= 0 || float64(status.DbSize) >= 0.70*float64(status.DbSizeQuota) || len(status.Errors) > 0,
	}
}
