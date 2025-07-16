import { useContext, useMemo } from "react";
import OnboardingContext from "./OnboardingContext";

export const useOnboardingContext = () => {
  const context = useContext(OnboardingContext);
  if (!context) {
    throw new Error(
      "useOnboardingContext must be used within an OnboardingProvider",
    );
  }

  const onboardingComplete = useMemo(() => {
    return (
      context.status === null ||
      (context.status?.devicePaired === true &&
        context.status?.poolConfigured === true)
    );
  }, [context.status]);

  const devicePaired = useMemo(() => {
    return context.status?.devicePaired === true;
  }, [context.status]);

  return {
    devicePaired,
    onboardingComplete,
    status: context.status,
    refetch: context.refetch,
  };
};

export default useOnboardingContext;
