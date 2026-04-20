package foremanimport

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	pb "github.com/block/proto-fleet/server/generated/grpc/foremanimport/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetimport"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/infrastructure/foreman"
	"github.com/block/proto-fleet/server/internal/infrastructure/networking"
	"golang.org/x/sync/errgroup"
)

// ForemanClient abstracts the Foreman REST API for testability.
type ForemanClient interface {
	ListMiners(ctx context.Context) ([]foreman.Miner, error)
	ListSiteMapGroups(ctx context.Context) ([]foreman.SiteMapGroup, error)
	ListSiteMapRacks(ctx context.Context) ([]foreman.SiteMapRack, error)
}

// ForemanClientFactory creates a ForemanClient from credentials.
type ForemanClientFactory func(apiKey, clientID string) ForemanClient

// Service fetches data from Foreman and delegates to the shared fleet importer.
type Service struct {
	importer    *fleetimport.Importer
	deviceStore interfaces.DeviceStore
	newClient   ForemanClientFactory
}

// NewService creates a new ForemanImport service.
func NewService(
	poolCreator fleetimport.PoolCreator,
	collectionManager fleetimport.CollectionManager,
	deviceStore interfaces.DeviceStore,
) *Service {
	return &Service{
		importer:    fleetimport.NewImporter(poolCreator, collectionManager, deviceStore),
		deviceStore: deviceStore,
		newClient: func(apiKey, clientID string) ForemanClient {
			return foreman.NewClient(apiKey, clientID)
		},
	}
}

// wrapForemanError logs the error (truncated) and returns a sanitized user-facing message.
func wrapForemanError(err error) error {
	errMsg := err.Error()
	if len(errMsg) > 1024 {
		errMsg = errMsg[:1024] + "..."
	}
	slog.Error("Foreman API request failed", "error", errMsg)
	var apiErr *foreman.APIError
	if errors.As(err, &apiErr) {
		if apiErr.IsUnauthorized() {
			return fleeterror.NewInvalidArgumentError("invalid Foreman API credentials")
		}
		return fleeterror.NewInternalError(
			fmt.Sprintf("Foreman returned an error (HTTP %d) — please try again", apiErr.StatusCode))
	}
	return fleeterror.NewInternalError("failed to fetch data from Foreman — please try again")
}

// fetchForemanData connects to the Foreman API and fetches miners, and optionally groups and racks.
func (s *Service) fetchForemanData(ctx context.Context, creds *pb.ForemanCredentials, fetchGroups, fetchRacks bool) ([]foreman.Miner, []foreman.SiteMapGroup, []foreman.SiteMapRack, error) {
	client := s.newClient(creds.ApiKey, creds.ClientId)

	var miners []foreman.Miner
	var groups []foreman.SiteMapGroup
	var racks []foreman.SiteMapRack

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		var fetchErr error
		miners, fetchErr = client.ListMiners(gctx)
		return fetchErr
	})
	if fetchGroups {
		g.Go(func() error {
			var fetchErr error
			groups, fetchErr = client.ListSiteMapGroups(gctx)
			return fetchErr
		})
	}
	if fetchRacks {
		g.Go(func() error {
			var fetchErr error
			racks, fetchErr = client.ListSiteMapRacks(gctx)
			return fetchErr
		})
	}
	if err := g.Wait(); err != nil {
		return nil, nil, nil, wrapForemanError(err)
	}

	return miners, groups, racks, nil
}

// ImportFromForeman validates Foreman credentials and returns miner IPs for discovery+pairing.
// Does NOT create pools/groups/racks — call CompleteImport after pairing.
func (s *Service) ImportFromForeman(ctx context.Context, req *pb.ImportFromForemanRequest) (*pb.ImportFromForemanResponse, error) {
	miners, _, _, err := s.fetchForemanData(ctx, req.Credentials, false, false)
	if err != nil {
		return nil, err
	}

	var foremanMiners []*pb.ForemanMiner
	for _, m := range miners {
		foremanMiners = append(foremanMiners, &pb.ForemanMiner{
			IpAddress:  m.IP,
			MacAddress: m.MAC,
			Name:       m.Name,
			Model:      parseModel(m.Type.Name),
		})
	}

	return &pb.ImportFromForemanResponse{
		Miners: foremanMiners,
	}, nil
}

// filterMinersByAllowlist resolves Foreman miner MACs to Fleet device identifiers
// and returns only miners whose device is in the allowlist.
func (s *Service) filterMinersByAllowlist(ctx context.Context, orgID int64, miners []foreman.Miner, allowedDeviceIDs []string) ([]foreman.Miner, error) {
	macs := make([]string, 0, len(miners))
	for _, m := range miners {
		if m.MAC != "" {
			macs = append(macs, m.MAC)
		}
	}
	macToDevice, err := s.deviceStore.GetPairedDevicesByMACAddresses(ctx, macs, orgID)
	if err != nil {
		return nil, fleeterror.NewInternalError("failed to resolve devices for import — please try again")
	}

	allowed := make(map[string]bool, len(allowedDeviceIDs))
	for _, id := range allowedDeviceIDs {
		allowed[id] = true
	}

	filtered := make([]foreman.Miner, 0, len(allowedDeviceIDs))
	for _, m := range miners {
		if m.MAC == "" {
			continue
		}
		if info, ok := macToDevice[networking.NormalizeMAC(m.MAC)]; ok && allowed[info.DeviceIdentifier] {
			filtered = append(filtered, m)
		}
	}
	return filtered, nil
}

// CompleteImport re-fetches Foreman data, creates pools/groups/racks, and assigns
// paired devices to their collections. Call after miners have been discovered and paired.
func (s *Service) CompleteImport(ctx context.Context, req *pb.CompleteImportRequest) (*pb.CompleteImportResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}
	orgID := info.OrganizationID

	miners, groups, racks, err := s.fetchForemanData(ctx, req.Credentials, req.ImportGroups, req.ImportRacks)
	if err != nil {
		return nil, err
	}

	// If an allowlist is provided, filter Foreman miners to only those whose
	// paired Fleet device is in the list. This scopes everything — pools,
	// groups, racks, and device assignments — to the selected subset.
	if len(req.PairedDeviceIdentifiers) > 0 {
		miners, err = s.filterMinersByAllowlist(ctx, orgID, miners, req.PairedDeviceIdentifiers)
		if err != nil {
			return nil, err
		}
	}

	data := normalizeForemanData(miners, groups, racks)

	// When importing a subset of miners, prune racks to only those referenced
	// by the filtered miners. Groups are kept since users may explicitly request them.
	if len(req.PairedDeviceIdentifiers) > 0 {
		data.PruneUnreferencedRacks()
	}

	if !req.ImportPools {
		data.Pools = nil
	}
	if !req.ImportGroups {
		data.Groups = nil
	}
	if !req.ImportRacks {
		data.Racks = nil
	}

	result := s.importer.Import(ctx, orgID, data)

	return &pb.CompleteImportResponse{
		PoolsCreated:    result.PoolsCreated,
		GroupsCreated:   result.GroupsCreated,
		RacksCreated:    result.RacksCreated,
		DevicesAssigned: result.DevicesAssigned,
		WorkerNamesSet:  result.WorkerNamesSet,
		MinerNamesSet:   result.MinerNamesSet,
	}, nil
}
