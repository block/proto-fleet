// Package containers projects controller observations into Fleet container cards.
package containers

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"time"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/containers/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Store interface {
	ListContainers(ctx context.Context, params sqlc.ListContainersParams) ([]sqlc.ListContainersRow, error)
}
type Service struct{ store Store }

func NewService(store Store) *Service { return &Service{store: store} }

// List authorizes each current site before exposing identity or telemetry.
func (s *Service) List(ctx context.Context, orgID int64, siteIDs []int64, unassigned bool, canRead func(*int64) (bool, error)) ([]*pb.Container, error) {
	rows, err := s.store.ListContainers(ctx, sqlc.ListContainersParams{OrgID: orgID, SiteIds: siteIDs, IncludeUnassigned: unassigned})
	if err != nil {
		return nil, err
	}
	result := make([]*pb.Container, 0, len(rows))
	now := time.Now()
	for _, r := range rows {
		var siteID *int64
		if r.SiteID.Valid {
			siteID = &r.SiteID.Int64
		}
		allowed, err := canRead(siteID)
		if err != nil {
			return nil, err
		}
		if !allowed {
			continue
		}
		c := &pb.Container{DeviceIdentifier: r.DeviceIdentifier, Name: r.Name, Stale: true, EmbeddedWebViewAvailable: r.EmbeddedWebViewAvailable}
		if r.IsActive && (r.PairingStatus == "PAIRED" || r.PairingStatus == "DEFAULT_PASSWORD") {
			c.Url = webViewURL(r.UrlScheme, r.IpAddress, r.Port)
		}
		if c.Name == "" {
			c.Name = "Container " + r.SerialNumber.String
		}
		if siteID != nil {
			c.Site = &commonpb.ResourceRef{Id: *siteID, Label: r.SiteName}
		}
		if r.ObservedAt.Valid {
			var snapshot sdk.ContainerSnapshot
			if err := json.Unmarshal(r.Snapshot.RawMessage, &snapshot); err != nil {
				return nil, fmt.Errorf("decode container observation: %w", err)
			}
			c.ObservedAt = timestamppb.New(r.ObservedAt.Time)
			c.Stale = now.Sub(r.ObservedAt.Time) > 2*time.Minute || r.ObservedAt.Time.After(now.Add(5*time.Second))
			for _, tank := range snapshot.Tanks {
				c.Tanks = append(c.Tanks, summarize(tank, now))
			}
		}
		if r.MetricsAt.Unix() > 0 {
			c.MetricsAt = timestamppb.New(r.MetricsAt)
		}
		metricsFresh := now.Sub(r.MetricsAt) <= 2*time.Minute && !r.MetricsAt.After(now.Add(5*time.Second))
		if metricsFresh && r.HashRateHs.Valid {
			c.HashrateHs = &r.HashRateHs.Float64
		}
		if metricsFresh && r.PowerW.Valid {
			c.PowerW = &r.PowerW.Float64
		}
		if metricsFresh && r.TempC.Valid {
			c.TemperatureC = &r.TempC.Float64
		}
		result = append(result, c)
	}
	return result, nil
}
func summarize(tank sdk.ContainerTank, now time.Time) *pb.TankHealth {
	out := &pb.TankHealth{Position: tank.Position}
	for _, m := range tank.Modules {
		if m.ObservedAt.IsZero() || now.Sub(m.ObservedAt) >= 2*time.Minute || m.ObservedAt.After(now.Add(5*time.Second)) {
			out.UnknownCount++
			continue
		}
		switch m.State {
		case sdk.ModuleHashing:
			out.HashingCount++
		case sdk.ModuleAttention:
			out.AttentionCount++
		case sdk.ModuleOffline:
			out.OfflineCount++
		case sdk.ModuleSleeping:
			out.SleepingCount++
		case sdk.ModuleIsolated:
			out.IsolatedCount++
		case sdk.ModuleUnknown:
			out.UnknownCount++
		default:
			out.UnknownCount++
		}
	}
	return out
}

// Container ports serve HTTP, unlike miners whose discovery ports can be RPC.
func webViewURL(scheme, ip, port string) string {
	if scheme != "http" && scheme != "https" {
		return ""
	}
	if _, err := netip.ParseAddr(ip); err != nil {
		return ""
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		return ""
	}
	u := url.URL{Scheme: scheme, Host: net.JoinHostPort(ip, port)}
	return u.String()
}
