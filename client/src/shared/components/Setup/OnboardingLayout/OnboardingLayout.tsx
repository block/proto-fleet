import { ReactNode } from "react";
import SetupHeader from "../SetupHeader";
import { useOnboardedStatus } from "@/protoFleet/api/useOnboardedStatus";
import StatusCircle, { statuses, variants } from "@/shared/components/StatusCircle";

type Step = {
  label: string;
  statusIndicator: string;
};

type OnboardingLayoutProps = {
  children: ReactNode;
  steps?: {
    [key: string]: Step;
  };
  currentStep?: string;
};

const OnboardingLayout = ({ children, steps, currentStep }: OnboardingLayoutProps) => {
  const onboardingStatus = useOnboardedStatus();

  return (
    <div className="min-h-screen bg-surface-base">
      <SetupHeader />
      <div className="relative px-6 pt-6 tablet:flex tablet:flex-row">
        {steps && currentStep ? (
          <div className="absolute w-30 phone:relative phone:mb-4 tablet:relative">
            <ol className="flex flex-col gap-2 phone:flex-row">
              {Object.entries(steps).map(([key, step]) => (
                <div key={key} className="flex h-8 items-center gap-2 text-emphasis-300 text-text-primary-50">
                  <StatusCircle
                    status={
                      currentStep == key
                        ? statuses.warning
                        : onboardingStatus?.[step.statusIndicator as keyof typeof onboardingStatus]
                          ? statuses.normal
                          : statuses.inactive
                    }
                    variant={variants.simple}
                    removeMargin={true}
                    width="w-2"
                  />
                  <span>{step.label}</span>
                </div>
              ))}
            </ol>
          </div>
        ) : null}
        <div className="mx-auto w-full max-w-xl tablet:max-w-160">{children}</div>
      </div>
    </div>
  );
};

export default OnboardingLayout;
