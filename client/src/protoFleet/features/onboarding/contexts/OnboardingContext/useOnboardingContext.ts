import { useContext } from "react";
import OnboardingContext from "./OnboardingContext";

export const useOnboardingContext = () => {
  const context = useContext(OnboardingContext);
  if (!context) {
    throw new Error(
      "useOnboardingContext must be used within an OnboardingProvider",
    );
  }

  return {
    status: context.status,
    refetch: context.refetch,
  };
};

export default useOnboardingContext;
