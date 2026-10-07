package deployment

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"go.etcd.io/etcd/api/v3/etcdserverpb"
	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc/metadata"

	"github.com/block/proto-fleet/server/internal/ha"
)

// RecoverEtcdRoot rotates only the administrator password, using the installed
// signing key as break-glass authority. The replacement must already be saved
// in a protected file; an interrupted request may still have committed it.
func RecoverEtcdRoot(ctx context.Context, envPath, passwordPath string) error {
	config, err := loadNodeConfig(envPath)
	if err != nil {
		return err
	}
	if err := validateNodeConfig(config); err != nil {
		return err
	}
	password, err := readPassword(passwordPath)
	if err != nil {
		return fmt.Errorf("read replacement root password: %w", err)
	}
	observerPassword, err := readPassword(filepath.Join(config.SecretsDir, fleetEtcdPasswordFile))
	if err != nil {
		return fmt.Errorf("read observer password: %w", err)
	}
	keyPath := filepath.Join(config.SecretsDir, "etcd-jwt.key")
	info, err := secureFileInfo(keyPath, 0o600)
	if err != nil {
		return err
	}
	if err := requireCurrentOwner(info, "etcd JWT key"); err != nil {
		return err
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return fmt.Errorf("read etcd JWT key: %w", err)
	}
	tlsConfig, err := ha.LoadServiceTLS(filepath.Join(config.SecretsDir, "service-ca.crt"))
	if err != nil {
		return err
	}
	client, err := clientv3.New(clientv3.Config{
		Endpoints: []string{"https://" + config.NodeIP + ":2379"}, TLS: tlsConfig, DialTimeout: 5 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("connect to local etcd: %w", err)
	}
	defer client.Close()
	return recoverEtcdRoot(ctx, client, keyPEM, observerPassword, password)
}

func recoverEtcdRoot(ctx context.Context, client *clientv3.Client, keyPEM []byte, observerPassword, password string) error {
	key, err := jwt.ParseRSAPrivateKeyFromPEM(keyPEM)
	if err != nil {
		return errors.New("invalid etcd JWT signing key")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	auth := etcdserverpb.NewAuthClient(client.ActiveConnection())
	observer, err := auth.Authenticate(ctx, &etcdserverpb.AuthenticateRequest{Name: "fleet-observer", Password: observerPassword})
	if err != nil {
		return fmt.Errorf("authenticate observer for recovery: %w", err)
	}
	claims := jwt.MapClaims{}
	if _, err := jwt.ParseWithClaims(observer.Token, claims, func(_ *jwt.Token) (any, error) { return &key.PublicKey, nil }, jwt.WithValidMethods([]string{"RS256"}), jwt.WithExpirationRequired()); err != nil {
		return errors.New("observer token does not match the installed JWT signing key")
	}
	revision, ok := claims["revision"].(float64)
	if !ok || revision <= 0 || claims["username"] != "fleet-observer" {
		return errors.New("observer token has invalid recovery claims")
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"username": "root", "revision": revision, "exp": time.Now().Add(time.Minute).Unix(),
	}).SignedString(key)
	if err != nil {
		return errors.New("sign etcd recovery token")
	}
	// Use the raw RPC so the client cannot replace the recovery token with a
	// cached service credential. Never persist or return this token.
	recoveryCtx := metadata.AppendToOutgoingContext(ctx, "token", token)
	state, err := auth.AuthStatus(recoveryCtx, &etcdserverpb.AuthStatusRequest{})
	if err != nil {
		return fmt.Errorf("verify etcd recovery authority: %w", err)
	}
	if !state.Enabled || state.AuthRevision != uint64(revision) {
		return errors.New("etcd authentication state changed; retry with the same replacement file")
	}
	if _, err := auth.UserChangePassword(recoveryCtx, &etcdserverpb.AuthUserChangePasswordRequest{Name: "root", Password: password}); err != nil {
		return fmt.Errorf("rotate etcd root password (retain replacement file; result may be uncertain): %w", err)
	}
	if _, err := auth.Authenticate(ctx, &etcdserverpb.AuthenticateRequest{Name: "root", Password: password}); err != nil {
		return fmt.Errorf("verify rotated etcd root password (retain replacement file): %w", err)
	}
	return nil
}
