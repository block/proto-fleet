package pairing_test

import (
	"fmt"
	"testing"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	discovery "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestContainerRPCDiscoveryPersistsEndpoints(t *testing.T) {
	for _, existingPort := range []string{"", "8181"} {
		t.Run("existing_http_"+existingPort, func(t *testing.T) {
			infra := testutil.InitializeDBServiceInfrastructure(t)
			admin := infra.DatabaseService.CreateSuperAdminUser()
			registerDiscoveryPortsPlugin(t, infra, "proto", []string{"80", "443", "8080", "9095"})
			discoverer := &MockDiscoverer{}
			service, ctx := setupTestService(t, infra, admin, nil, discoverer)
			store := sqlstores.NewSQLDiscoveredDeviceStore(infra.ServiceProvider.DB)
			ip := "172.16.71.20"
			if existingPort != "" {
				_, err := store.SaveDirect(ctx, discovery.DeviceOrgIdentifier{OrgID: admin.OrganizationID, DeviceIdentifier: "existing-container"}, &discovery.DiscoveredDevice{Device: pb.Device{
					DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_CONTAINER, DriverName: "proto", Model: "CT1", Manufacturer: "Proto", IpAddress: ip, Port: existingPort, UrlScheme: "https",
				}})
				require.NoError(t, err)
			}
			for range 2 {
				device := &discovery.DiscoveredDevice{Device: pb.Device{DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_CONTAINER, DriverName: "proto", Model: "CT1", Manufacturer: "Proto", IpAddress: "203.0.113.99", Port: "8080", UrlScheme: "http", RpcPort: 9095, RpcTls: true}}
				discoverer.On("Discover", mock.Anything, ip, "9095").Return(device, nil).Once()
				results, err := service.DiscoverWithIPList(ctx, &pb.IPListModeRequest{IpAddresses: []string{ip}, Ports: []string{"9095"}})
				require.NoError(t, err)
				var found []*pb.Device
				for result := range results {
					found = append(found, result.Devices...)
				}
				require.Len(t, found, 1)
				saved, err := store.GetDevice(ctx, discovery.DeviceOrgIdentifier{OrgID: admin.OrganizationID, DeviceIdentifier: found[0].DeviceIdentifier})
				require.NoError(t, err)
				require.Equal(t, ip, saved.IpAddress)
				require.Equal(t, int32(9095), saved.RpcPort)
				require.True(t, saved.RpcTls)
				if existingPort == "" {
					require.Equal(t, "8080", saved.Port)
					require.Equal(t, "http", saved.UrlScheme)
				} else {
					require.Equal(t, "existing-container", saved.DeviceIdentifier)
					require.Equal(t, existingPort, saved.Port)
					require.Equal(t, "https", saved.UrlScheme)
				}
			}
			var count int
			require.NoError(t, infra.ServiceProvider.DB.QueryRowContext(ctx, "SELECT count(*) FROM discovered_device WHERE org_id=$1 AND ip_address=$2 AND deleted_at IS NULL", admin.OrganizationID, ip).Scan(&count))
			require.Equal(t, 1, count)
		})
	}
}

func TestContainerRPCDiscoveryRejectsAmbiguousExistingContainers(t *testing.T) {
	infra := testutil.InitializeDBServiceInfrastructure(t)
	admin := infra.DatabaseService.CreateSuperAdminUser()
	registerDiscoveryPortsPlugin(t, infra, "proto", []string{"8080", "9095"})
	discoverer := &MockDiscoverer{}
	service, ctx := setupTestService(t, infra, admin, nil, discoverer)
	store := sqlstores.NewSQLDiscoveredDeviceStore(infra.ServiceProvider.DB)
	for _, port := range []string{"8080", "8181"} {
		_, err := store.SaveDirect(ctx, discovery.DeviceOrgIdentifier{OrgID: admin.OrganizationID, DeviceIdentifier: "container-" + port}, &discovery.DiscoveredDevice{Device: pb.Device{
			DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_CONTAINER, DriverName: "proto", Model: "CT1", Manufacturer: "Proto", IpAddress: "172.16.71.20", Port: port, UrlScheme: "http",
		}})
		require.NoError(t, err)
	}
	device := &discovery.DiscoveredDevice{Device: pb.Device{DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_CONTAINER, DriverName: "proto", Model: "CT1", Manufacturer: "Proto", Port: "8080", UrlScheme: "http", RpcPort: 9095}}
	discoverer.On("Discover", mock.Anything, "172.16.71.20", "9095").Return(device, nil).Once()
	results, err := service.DiscoverWithIPList(ctx, &pb.IPListModeRequest{IpAddresses: []string{"172.16.71.20"}, Ports: []string{"9095"}})
	require.NoError(t, err)
	var warnings []string
	for result := range results {
		require.Empty(t, result.Devices, "ambiguous matches must not select a container")
		if result.Warning != "" {
			warnings = append(warnings, result.Warning)
		}
	}
	require.NotEmpty(t, warnings)
	var count int
	require.NoError(t, infra.ServiceProvider.DB.QueryRowContext(ctx, "SELECT count(*) FROM discovered_device WHERE org_id=$1 AND ip_address=$2 AND deleted_at IS NULL", admin.OrganizationID, "172.16.71.20").Scan(&count))
	require.Equal(t, 2, count, fmt.Sprint(warnings))
}

func TestProtectedContainerRediscoveryKeepsVerifiedIdentity(t *testing.T) {
	// Only a paired device has an identity that pairing verified. An unpaired
	// row goes blank, so a replaced controller can pair under its own ID.
	for _, tc := range []struct {
		name         string
		paired       bool
		wantSerial   string
		wantFirmware string
	}{
		{name: "paired", paired: true, wantSerial: "controller-a", wantFirmware: "v1"},
		{name: "unpaired"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			infra := testutil.InitializeDBServiceInfrastructure(t)
			admin := infra.DatabaseService.CreateSuperAdminUser()
			registerDiscoveryPortsPlugin(t, infra, "proto", []string{"80", "443", "8080", "9095"})
			discoverer := &MockDiscoverer{}
			service, ctx := setupTestService(t, infra, admin, nil, discoverer)
			store := sqlstores.NewSQLDiscoveredDeviceStore(infra.ServiceProvider.DB)
			ip := "172.16.71.21"
			_, err := store.SaveDirect(ctx, discovery.DeviceOrgIdentifier{OrgID: admin.OrganizationID, DeviceIdentifier: "protected-container"}, &discovery.DiscoveredDevice{Device: pb.Device{
				DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_CONTAINER, DriverName: "proto", Model: "CT1", Manufacturer: "Proto",
				IpAddress: ip, Port: "8080", UrlScheme: "http", RpcPort: 9095, SerialNumber: "controller-a", FirmwareVersion: "v1",
			}})
			require.NoError(t, err)
			if tc.paired {
				_, err = infra.ServiceProvider.DB.ExecContext(ctx, `INSERT INTO device (org_id, discovered_device_id, device_identifier, mac_address)
					SELECT org_id, id, device_identifier, '' FROM discovered_device WHERE org_id = $1 AND device_identifier = $2`,
					admin.OrganizationID, "protected-container")
				require.NoError(t, err)
			}

			// A controller that requires a token answers discovery without its identity.
			unidentified := &discovery.DiscoveredDevice{Device: pb.Device{DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_CONTAINER, DriverName: "proto", Model: "CT1", Manufacturer: "Proto", IpAddress: ip, Port: "8080", UrlScheme: "http", RpcPort: 9095}}
			discoverer.On("Discover", mock.Anything, ip, "9095").Return(unidentified, nil).Once()
			results, err := service.DiscoverWithIPList(ctx, &pb.IPListModeRequest{IpAddresses: []string{ip}, Ports: []string{"9095"}})
			require.NoError(t, err)
			var found []*pb.Device
			for result := range results {
				found = append(found, result.Devices...)
			}
			require.Len(t, found, 1)
			require.Equal(t, "protected-container", found[0].DeviceIdentifier)
			saved, err := store.GetDevice(ctx, discovery.DeviceOrgIdentifier{OrgID: admin.OrganizationID, DeviceIdentifier: "protected-container"})
			require.NoError(t, err)
			require.Equal(t, tc.wantSerial, saved.SerialNumber)
			require.Equal(t, tc.wantFirmware, saved.FirmwareVersion)
		})
	}
}
