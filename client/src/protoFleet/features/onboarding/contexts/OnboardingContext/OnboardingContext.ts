import { createContext } from "react";
import type { FleetOnboardingStatus } from "@/protoFleet/api/generated/onboarding/v1/onboarding_pb";

interface OnboardingContextType {
  status: FleetOnboardingStatus | null;
  refetch: () => Promise<FleetOnboardingStatus | null>;
}

const OnboardingContext = createContext<OnboardingContextType | null>(null);

export default OnboardingContext;
