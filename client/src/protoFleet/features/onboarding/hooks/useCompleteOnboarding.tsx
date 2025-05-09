import { useEffect } from "react";
import { useNavigate } from "react-router-dom";
import { FleetOnboardingStatus } from "@/protoFleet/api/generated/onboarding/v1/onboarding_pb";
import { useOnboardedStatus } from "@/protoFleet/api/useOnboardedStatus";

const ONBOARDING_ROUTES = {
  devicePaired: "/onboarding/miners",
  // // TODO: networkConfigured is always false currently
  // networkConfigured: "/onboarding/network",
  poolConfigured: "/onboarding/mining-pool",
};

const useCompleteOnboarding = () => {
  const navigate = useNavigate();
  const onboardingStatus = useOnboardedStatus();

  useEffect(() => {
    if (onboardingStatus === null) {
      return;
    }

    // iterating through ONBOARDING ROUTES to insure correct order of steps
    const currentStep = Object.keys(ONBOARDING_ROUTES).find(
      (key) => onboardingStatus[key as keyof FleetOnboardingStatus] === false,
    ) as keyof typeof ONBOARDING_ROUTES;

    if (currentStep && ONBOARDING_ROUTES[currentStep]) {
      navigate(ONBOARDING_ROUTES[currentStep]);
    }
  }, [onboardingStatus, navigate]);
};

export default useCompleteOnboarding;
