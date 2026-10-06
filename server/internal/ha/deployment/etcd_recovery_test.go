package deployment

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.etcd.io/etcd/api/v3/etcdserverpb"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/block/proto-fleet/server/internal/ha"
)

// Opt-in: a disposable, loopback-only instance of the exact shipped etcd image.
// No host runtime or credentials are used.
func TestEtcdRootRecoverySmoke(t *testing.T) {
	if os.Getenv("PROTO_FLEET_ETCD_RECOVERY_SMOKE") != "1" {
		t.Skip("set PROTO_FLEET_ETCD_RECOVERY_SMOKE=1 with Docker available")
	}
	dir := filepath.Join(t.TempDir(), "secrets")
	require.NoError(t, generateSecrets(dir, [3]string{"127.0.0.1", "127.0.0.2", "127.0.0.3"}, "", true))
	secrets := filepath.Join(dir, "ha-a")
	compose, err := os.ReadFile("../../../../deployment-files/ha/compose.yaml")
	require.NoError(t, err)
	image := regexp.MustCompile(`image: (gcr.io/etcd-development/etcd:\S+)`).FindSubmatch(compose)
	require.Len(t, image, 2)
	output, err := exec.Command("docker", "run", "--rm", "-d", "-p", "127.0.0.1::2379", "-v", secrets+":/secrets:ro", string(image[1]),
		"/usr/local/bin/etcd", "--data-dir=/tmp/etcd", "--listen-client-urls=https://0.0.0.0:2379", "--advertise-client-urls=https://127.0.0.1:2379",
		"--cert-file=/secrets/etcd-server.crt", "--key-file=/secrets/etcd-server.key",
		"--auth-token=jwt,pub-key=/secrets/etcd-jwt.pub,priv-key=/secrets/etcd-jwt.key,sign-method=RS256,ttl=10m").CombinedOutput()
	require.NoError(t, err, string(output))
	container := strings.TrimSpace(string(output))
	t.Cleanup(func() { require.NoError(t, exec.Command("docker", "rm", "-f", container).Run()) })
	port, err := exec.Command("docker", "port", container, "2379").Output()
	require.NoError(t, err)
	tlsConfig, err := ha.LoadServiceTLS(filepath.Join(secrets, "service-ca.crt"))
	require.NoError(t, err)
	config := clientv3.Config{Endpoints: []string{"https://" + strings.TrimSpace(string(port))}, TLS: tlsConfig, DialTimeout: 5 * time.Second}
	client, err := clientv3.New(config)
	require.NoError(t, err)
	defer client.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	_, err = client.Status(ctx, config.Endpoints[0])
	require.NoError(t, err)
	keyPEM, err := os.ReadFile(filepath.Join(secrets, "etcd-jwt.key"))
	require.NoError(t, err)
	require.ErrorContains(t, recoverEtcdRoot(ctx, client, keyPEM, "observer-test", "unused"), "authenticate observer")
	require.NoError(t, bootstrapEtcdAuth(ctx, &etcdAuthClient{client: client, endpoint: config.Endpoints[0], tlsConfig: tlsConfig}, "old-root", "patroni-test", "observer-test"))
	config.Username, config.Password = "root", "old-root"
	root, err := clientv3.New(config)
	require.NoError(t, err)
	defer root.Close()
	lease, err := root.Grant(ctx, 120)
	require.NoError(t, err)
	_, err = root.Put(ctx, patroniDCSPath+"leader", "ha-a", clientv3.WithLease(lease.ID))
	require.NoError(t, err)
	config.Username, config.Password = "fleet-observer", "observer-test"
	observer, err := clientv3.New(config)
	require.NoError(t, err)
	defer observer.Close()
	_, err = observer.UserChangePassword(ctx, "root", "forbidden")
	require.Error(t, err)
	// A different cluster's key cannot grant administrative access.
	other := filepath.Join(t.TempDir(), "other")
	require.NoError(t, generateSecrets(other, [3]string{"127.0.0.1", "127.0.0.2", "127.0.0.3"}, "", true))
	wrongKey, err := os.ReadFile(filepath.Join(other, "ha-a", "etcd-jwt.key"))
	require.NoError(t, err)
	require.Error(t, recoverEtcdRoot(ctx, client, wrongKey, "observer-test", "forbidden"))
	require.Error(t, recoverEtcdRoot(ctx, client, keyPEM, "wrong-observer-password", "forbidden"))
	status, err := root.Status(ctx, config.Endpoints[0])
	require.NoError(t, err)
	_, err = etcdserverpb.NewMaintenanceClient(root.ActiveConnection()).Alarm(ctx, &etcdserverpb.AlarmRequest{
		Action: etcdserverpb.AlarmRequest_ACTIVATE, MemberID: status.Header.MemberId, Alarm: etcdserverpb.AlarmType_NOSPACE,
	})
	require.NoError(t, err)
	require.NoError(t, recoverEtcdRoot(ctx, client, keyPEM, "observer-test", "replacement-root"))
	_, err = client.Authenticate(ctx, "root", "old-root")
	require.Error(t, err)
	config.Username, config.Password = "root", "replacement-root"
	recovered, err := clientv3.New(config)
	require.NoError(t, err)
	defer recovered.Close()
	state, err := recovered.AuthStatus(ctx)
	require.NoError(t, err)
	require.True(t, state.Enabled)
	alarms, err := recovered.AlarmList(ctx)
	require.NoError(t, err)
	require.Len(t, alarms.Alarms, 1)
	require.Equal(t, etcdserverpb.AlarmType_NOSPACE, alarms.Alarms[0].Alarm)

	data, err := observer.Get(ctx, patroniDCSPath+"leader")
	require.NoError(t, err)
	require.Len(t, data.Kvs, 1)
	require.Equal(t, "ha-a", string(data.Kvs[0].Value))
	remaining, err := recovered.TimeToLive(ctx, lease.ID)
	require.NoError(t, err)
	require.Positive(t, remaining.TTL)
	_, err = recovered.AlarmDisarm(ctx, (*clientv3.AlarmMember)(alarms.Alarms[0]))
	require.NoError(t, err)
	_, err = observer.Put(ctx, patroniDCSPath+"leader", "forbidden")
	require.ErrorContains(t, err, "permission denied")
	_, err = observer.UserChangePassword(ctx, "root", "forbidden")
	require.Error(t, err)
	_, err = recovered.Defragment(ctx, config.Endpoints[0])
	require.NoError(t, err)
	snapshot, err := recovered.Snapshot(ctx)
	require.NoError(t, err)
	size, err := io.Copy(io.Discard, snapshot)
	require.NoError(t, err)
	require.Positive(t, size)
	require.NoError(t, snapshot.Close())
}
