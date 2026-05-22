package middleware_test

import (
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/grpc/activity/v1/activityv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/apikey/v1/apikeyv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/auth/v1/authv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/buildings/v1/buildingsv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/collection/v1/collectionv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/curtailment/v1/curtailmentv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/device_set/v1/device_setv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/errors/v1/errorsv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1/fleetmanagementv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodeadmin/v1/fleetnodeadminv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/foremanimport/v1/foremanimportv1connect"
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
	{buildingsv1connect.BuildingServiceName, reflect.TypeOf((*buildingsv1connect.BuildingServiceHandler)(nil)).Elem()},
	{collectionv1connect.DeviceCollectionServiceName, reflect.TypeOf((*collectionv1connect.DeviceCollectionServiceHandler)(nil)).Elem()},
	{curtailmentv1connect.CurtailmentServiceName, reflect.TypeOf((*curtailmentv1connect.CurtailmentServiceHandler)(nil)).Elem()},
	{device_setv1connect.DeviceSetServiceName, reflect.TypeOf((*device_setv1connect.DeviceSetServiceHandler)(nil)).Elem()},
	{errorsv1connect.ErrorQueryServiceName, reflect.TypeOf((*errorsv1connect.ErrorQueryServiceHandler)(nil)).Elem()},
	{fleetmanagementv1connect.FleetManagementServiceName, reflect.TypeOf((*fleetmanagementv1connect.FleetManagementServiceHandler)(nil)).Elem()},
	{fleetnodeadminv1connect.FleetNodeAdminServiceName, reflect.TypeOf((*fleetnodeadminv1connect.FleetNodeAdminServiceHandler)(nil)).Elem()},
	{fleetnodegatewayv1connect.FleetNodeGatewayServiceName, reflect.TypeOf((*fleetnodegatewayv1connect.FleetNodeGatewayServiceHandler)(nil)).Elem()},
	{foremanimportv1connect.ForemanImportServiceName, reflect.TypeOf((*foremanimportv1connect.ForemanImportServiceHandler)(nil)).Elem()},
	{minercommandv1connect.MinerCommandServiceName, reflect.TypeOf((*minercommandv1connect.MinerCommandServiceHandler)(nil)).Elem()},
	{networkinfov1connect.NetworkInfoServiceName, reflect.TypeOf((*networkinfov1connect.NetworkInfoServiceHandler)(nil)).Elem()},
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
// ProcedurePermissions, or ProceduresPendingGate. Adding a new RPC
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
	addMap("ProceduresPendingGate", middleware.ProceduresPendingGate)

	var missing []string
	for _, p := range allRegisteredProcedures() {
		if _, ok := classified[p]; !ok {
			missing = append(missing, p)
		}
	}
	require.Empty(t, missing,
		"every procedure registered on the production Connect mux must be classified by RBAC; "+
			"add each of the procedures below to UnauthenticatedProcedures, FleetNodeAuthenticatedProcedures, "+
			"ProcedurePermissions, or ProceduresPendingGate:\n  %s",
		fmt.Sprintf("%q", missing))
}

// TestRPCContract_ProcedurePermissionsKeysAreInCatalog asserts every
// permission key referenced by ProcedurePermissions exists in the
// catalog. Stale keys (typos, removed permissions) get caught at test
// time rather than slipping into production and quietly failing
// every gate.
//
// In PR 2b (infrastructure-only) ProcedurePermissions is empty so
// this test is a no-op; it becomes load-bearing in PR 2c onwards.
func TestRPCContract_ProcedurePermissionsKeysAreInCatalog(t *testing.T) {
	// Imported via the test so the package compiles when
	// ProcedurePermissions is empty.
	for procedure, key := range middleware.ProcedurePermissions {
		if !permissionKeyExists(key) {
			t.Errorf("procedure %q gated by %q, which is not in the permission catalog", procedure, key)
		}
	}
}
