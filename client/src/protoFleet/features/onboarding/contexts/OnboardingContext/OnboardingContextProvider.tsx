import { ReactNode } from "react";
import OnboardingContext from "./OnboardingContext";
import { useOnboardedStatus } from "@/protoFleet/api/useOnboardedStatus";

interface OnboardingProviderProps {
  children: ReactNode;
}

const OnboardingProvider = ({ children }: OnboardingProviderProps) => {
  const { status, refetch } = useOnboardedStatus();

  return (
    <OnboardingContext.Provider value={{ status, refetch }}>
      {children}
    </OnboardingContext.Provider>
  );
};

export default OnboardingProvider;
