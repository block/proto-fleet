import { createClient } from "@connectrpc/connect";
import { OnboardingService } from "./generated/onboarding/v1/onboarding_pb";
import { transport } from "./transport";

const onboardingServiceClient = createClient(OnboardingService, transport);

export { onboardingServiceClient };
