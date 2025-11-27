package interceptors

import (
	"github.com/btc-mining/proto-fleet/server/generated/grpc/auth/v1/authv1connect"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/networkinfo/v1/networkinfov1connect"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/onboarding/v1/onboardingv1connect"
)

var UnauthenticatedProcedures = []string{
	"/health",
	"/grpc.reflection.v1alpha.ServerReflection/ServerReflectionInfo",
	authv1connect.AuthServiceAuthenticateProcedure,
	onboardingv1connect.OnboardingServiceCreateAdminLoginProcedure,
	onboardingv1connect.OnboardingServiceGetFleetOnboardingStatusProcedure,
	networkinfov1connect.NetworkInfoServiceGetNetworkInfoProcedure,
}
