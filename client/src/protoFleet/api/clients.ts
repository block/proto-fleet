import { createClient } from "@connectrpc/connect";
import { AuthService } from "./generated/auth/v1/auth_pb";
import { FleetManagementService } from "./generated/fleetmanagement/v1/fleetmanagement_pb";
import { OnboardingService } from "./generated/onboarding/v1/onboarding_pb";
import { transport } from "./transport";

const authClient = createClient(AuthService, transport);
const fleetManagementClient = createClient(FleetManagementService, transport);
const onboardingClient = createClient(OnboardingService, transport);

export { authClient, fleetManagementClient, onboardingClient };
