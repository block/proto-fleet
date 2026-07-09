package middleware_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/grpc/activity/v1/activityv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/alerts/v1/alertsv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/apikey/v1/apikeyv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/auth/v1/authv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/authz/v1/authzv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/buildings/v1/buildingsv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/collection/v1/collectionv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/curtailment/v1/curtailmentv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/device_set/v1/device_setv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/errors/v1/errorsv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1/fleetmanagementv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodeadmin/v1/fleetnodeadminv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/foremanimport/v1/foremanimportv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/infrastructure/v1/infrastructurev1connect"
	"github.com/block/proto-fleet/server/generated/grpc/minercommand/v1/minercommandv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/networkinfo/v1/networkinfov1connect"
	"github.com/block/proto-fleet/server/generated/grpc/onboarding/v1/onboardingv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/pairing/v1/pairingv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/pools/v1/poolsv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/schedule/v1/schedulev1connect"
	"github.com/block/proto-fleet/server/generated/grpc/serverlog/v1/serverlogv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/sites/v1/sitesv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/telemetry/v1/telemetryv1connect"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/handlers/interceptors"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

// permissionKeyExists returns true when the supplied permission key
// is registered in the authz catalog. Wraps authz.Lookup to keep the
// test code readable.
func permissionKeyExists(key string) bool {
	_, ok := authz.Lookup(key)
	return ok
}

// registeredServices mirrors the Connect handler registrations in
// cmd/fleetd/main.go. Each entry pairs a service's fully-qualified
// name with the type of its generated handler interface so the
// contract test can enumerate methods via reflection without needing
// to construct real handler implementations.
//
// Adding a new service to the production mux requires adding it here
// too; otherwise the contract test cannot reach its procedures and
// the auto-discovery property collapses. The reverse direction is
// caught by the build — removing a service makes the import unused.
var registeredServices = []struct {
	name      string
	ifaceType reflect.Type
}{
	{activityv1connect.ActivityServiceName, reflect.TypeOf((*activityv1connect.ActivityServiceHandler)(nil)).Elem()},
	{apikeyv1connect.ApiKeyServiceName, reflect.TypeOf((*apikeyv1connect.ApiKeyServiceHandler)(nil)).Elem()},
	{authv1connect.AuthServiceName, reflect.TypeOf((*authv1connect.AuthServiceHandler)(nil)).Elem()},
	{authzv1connect.AuthzServiceName, reflect.TypeOf((*authzv1connect.AuthzServiceHandler)(nil)).Elem()},
	{buildingsv1connect.BuildingServiceName, reflect.TypeOf((*buildingsv1connect.BuildingServiceHandler)(nil)).Elem()},
	{collectionv1connect.DeviceCollectionServiceName, reflect.TypeOf((*collectionv1connect.DeviceCollectionServiceHandler)(nil)).Elem()},
	{curtailmentv1connect.CurtailmentServiceName, reflect.TypeOf((*curtailmentv1connect.CurtailmentServiceHandler)(nil)).Elem()},
	{device_setv1connect.DeviceSetServiceName, reflect.TypeOf((*device_setv1connect.DeviceSetServiceHandler)(nil)).Elem()},
	{errorsv1connect.ErrorQueryServiceName, reflect.TypeOf((*errorsv1connect.ErrorQueryServiceHandler)(nil)).Elem()},
	{fleetmanagementv1connect.FleetManagementServiceName, reflect.TypeOf((*fleetmanagementv1connect.FleetManagementServiceHandler)(nil)).Elem()},
	{fleetnodeadminv1connect.FleetNodeAdminServiceName, reflect.TypeOf((*fleetnodeadminv1connect.FleetNodeAdminServiceHandler)(nil)).Elem()},
	{fleetnodegatewayv1connect.FleetNodeGatewayServiceName, reflect.TypeOf((*fleetnodegatewayv1connect.FleetNodeGatewayServiceHandler)(nil)).Elem()},
	{foremanimportv1connect.ForemanImportServiceName, reflect.TypeOf((*foremanimportv1connect.ForemanImportServiceHandler)(nil)).Elem()},
	{infrastructurev1connect.InfrastructureServiceName, reflect.TypeOf((*infrastructurev1connect.InfrastructureServiceHandler)(nil)).Elem()},
	{minercommandv1connect.MinerCommandServiceName, reflect.TypeOf((*minercommandv1connect.MinerCommandServiceHandler)(nil)).Elem()},
	{networkinfov1connect.NetworkInfoServiceName, reflect.TypeOf((*networkinfov1connect.NetworkInfoServiceHandler)(nil)).Elem()},
	{alertsv1connect.ChannelServiceName, reflect.TypeOf((*alertsv1connect.ChannelServiceHandler)(nil)).Elem()},
	{alertsv1connect.RuleServiceName, reflect.TypeOf((*alertsv1connect.RuleServiceHandler)(nil)).Elem()},
	{alertsv1connect.MaintenanceWindowServiceName, reflect.TypeOf((*alertsv1connect.MaintenanceWindowServiceHandler)(nil)).Elem()},
	{alertsv1connect.HistoryServiceName, reflect.TypeOf((*alertsv1connect.HistoryServiceHandler)(nil)).Elem()},
	{onboardingv1connect.OnboardingServiceName, reflect.TypeOf((*onboardingv1connect.OnboardingServiceHandler)(nil)).Elem()},
	{pairingv1connect.PairingServiceName, reflect.TypeOf((*pairingv1connect.PairingServiceHandler)(nil)).Elem()},
	{poolsv1connect.PoolsServiceName, reflect.TypeOf((*poolsv1connect.PoolsServiceHandler)(nil)).Elem()},
	{schedulev1connect.ScheduleServiceName, reflect.TypeOf((*schedulev1connect.ScheduleServiceHandler)(nil)).Elem()},
	{serverlogv1connect.ServerLogServiceName, reflect.TypeOf((*serverlogv1connect.ServerLogServiceHandler)(nil)).Elem()},
	{sitesv1connect.SiteServiceName, reflect.TypeOf((*sitesv1connect.SiteServiceHandler)(nil)).Elem()},
	{telemetryv1connect.TelemetryServiceName, reflect.TypeOf((*telemetryv1connect.TelemetryServiceHandler)(nil)).Elem()},
}

func allRegisteredProcedures() []string {
	var out []string
	for _, svc := range registeredServices {
		for i := range svc.ifaceType.NumMethod() {
			method := svc.ifaceType.Method(i)
			out = append(out, fmt.Sprintf("/%s/%s", svc.name, method.Name))
		}
	}
	sort.Strings(out)
	return out
}

// TestRPCContract_EveryRegisteredProcedureIsClassified asserts every
// Connect procedure registered on the production mux appears in
// exactly one of: UnauthenticatedProcedures, FleetNodeAuthenticatedProcedures,
// ProcedurePermissions, or ProceduresPendingMigration. Adding a new RPC
// without classifying it fails this test loudly; a procedure that
// shows up in two lists is also flagged.
func TestRPCContract_EveryRegisteredProcedureIsClassified(t *testing.T) {
	type bucket struct{ name string }
	classified := make(map[string]bucket)

	add := func(name string, items []string) {
		for _, p := range items {
			if existing, ok := classified[p]; ok {
				t.Errorf("procedure %q listed in both %s and %s", p, existing.name, name)
				continue
			}
			classified[p] = bucket{name: name}
		}
	}
	addMap := func(name string, m map[string]string) {
		for p := range m {
			if existing, ok := classified[p]; ok {
				t.Errorf("procedure %q listed in both %s and %s", p, existing.name, name)
				continue
			}
			classified[p] = bucket{name: name}
		}
	}

	add("UnauthenticatedProcedures", interceptors.UnauthenticatedProcedures)
	add("FleetNodeAuthenticatedProcedures", interceptors.FleetNodeAuthenticatedProcedures)
	addMap("ProcedurePermissions", middleware.ProcedurePermissions)
	addMap("ProceduresPendingMigration", middleware.ProceduresPendingMigration)

	procedures := allRegisteredProcedures()
	// Guard against silent pass when reflection finds nothing — e.g.
	// connect-go renames the *Handler interfaces or every service is
	// accidentally dropped from registeredServices.
	require.NotEmpty(t, procedures,
		"reflection discovered zero procedures across registeredServices; "+
			"the contract test would pass vacuously")

	var missing []string
	for _, p := range procedures {
		if _, ok := classified[p]; !ok {
			missing = append(missing, p)
		}
	}
	require.Empty(t, missing,
		"every procedure registered on the production Connect mux must be classified by RBAC; "+
			"add each of the procedures below to UnauthenticatedProcedures, FleetNodeAuthenticatedProcedures, "+
			"ProcedurePermissions, or ProceduresPendingMigration:\n  %s",
		fmt.Sprintf("%q", missing))
}

// TestRPCContract_ProcedurePermissionsKeysAreInCatalog asserts every
// permission key referenced by ProcedurePermissions exists in the
// catalog. Stale keys (typos, removed permissions) get caught at test
// time rather than slipping into production and quietly failing
// every gate.
func TestRPCContract_ProcedurePermissionsKeysAreInCatalog(t *testing.T) {
	// Imported via the test so the package compiles when
	// ProcedurePermissions is empty.
	for procedure, key := range middleware.ProcedurePermissions {
		if !permissionKeyExists(key) {
			t.Errorf("procedure %q gated by %q, which is not in the permission catalog", procedure, key)
		}
	}
}

func TestRPCContract_FirmwareUpdateUsesFirmwareUpdatePermission(t *testing.T) {
	require.Equal(t,
		authz.PermMinerFirmwareUpdate,
		middleware.ProcedurePermissions[minercommandv1connect.MinerCommandServiceFirmwareUpdateProcedure],
	)
}

// mainConnectMountRe captures both the connect-package shortname and
// the service-handler short name (e.g. "authv1connect" + "Auth") from
// every Connect handler mount in main.go like
// `mux.Handle(authv1connect.NewAuthServiceHandler(...)`. Capturing both
// keeps the guard honest at constructor granularity: a second service
// added to an already-mounted v1connect package will not pass the check
// just because its package is present.
var mainConnectMountRe = regexp.MustCompile(`\b(\w+v\d+connect)\.New(\w+)ServiceHandler\(`)

// mountIdentifier formats a (pkg, ServiceShortName) tuple into the
// canonical "<pkg>.<ServiceShortName>" used to compare main.go mounts
// against registeredServices.
func mountIdentifier(pkg, serviceShortName string) string {
	return pkg + "." + serviceShortName
}

// TestRPCContract_RegisteredServicesMatchMainMux asserts that every
// connect handler mounted in cmd/fleetd/main.go is enumerated in
// registeredServices, and vice versa. The comparison runs at
// constructor granularity (`<pkg>.<ServiceShortName>`), so adding a
// second service to an existing v1connect package without listing it
// here fails the test — package-level granularity would silently miss
// that case.
func TestRPCContract_RegisteredServicesMatchMainMux(t *testing.T) {
	mainPath := locateMainGo(t)
	src, err := os.ReadFile(mainPath)
	require.NoError(t, err, "reading %s", mainPath)

	mounted := make(map[string]struct{})
	for _, m := range mainConnectMountRe.FindAllSubmatch(src, -1) {
		mounted[mountIdentifier(string(m[1]), string(m[2]))] = struct{}{}
	}
	require.NotEmpty(t, mounted,
		"regex found no connect handler mounts in %s — pattern may have drifted", mainPath)

	registered := make(map[string]struct{}, len(registeredServices))
	for _, svc := range registeredServices {
		// reflect.Type.PkgPath returns the import path; the last segment
		// is the v1connect package shortname that appears in main.go.
		// reflect.Type.Name returns "<ServiceShortName>ServiceHandler".
		pkg := svc.ifaceType.PkgPath()
		pkg = pkg[strings.LastIndex(pkg, "/")+1:]
		shortName := strings.TrimSuffix(svc.ifaceType.Name(), "ServiceHandler")
		registered[mountIdentifier(pkg, shortName)] = struct{}{}
	}

	var missingFromTest, missingFromMux []string
	for id := range mounted {
		if _, ok := registered[id]; !ok {
			missingFromTest = append(missingFromTest, id)
		}
	}
	for id := range registered {
		if _, ok := mounted[id]; !ok {
			missingFromMux = append(missingFromMux, id)
		}
	}
	sort.Strings(missingFromTest)
	sort.Strings(missingFromMux)

	require.Empty(t, missingFromTest,
		"services mounted in main.go but missing from registeredServices "+
			"(their procedures escape the classification check): %v", missingFromTest)
	require.Empty(t, missingFromMux,
		"services in registeredServices but not mounted in main.go "+
			"(stale entries — drop them): %v", missingFromMux)
}

// locateMainGo resolves cmd/fleetd/main.go relative to this test
// file's location via runtime.Caller, which is stable regardless of
// the test runner's working directory.
func locateMainGo(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")
	// thisFile = .../server/internal/handlers/middleware/rpc_permissions_test.go
	// main.go  = .../server/cmd/fleetd/main.go
	serverRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
	mainPath := filepath.Join(serverRoot, "cmd", "fleetd", "main.go")
	_, err := os.Stat(mainPath)
	require.NoError(t, err, "expected main.go at %s", mainPath)
	return mainPath
}
