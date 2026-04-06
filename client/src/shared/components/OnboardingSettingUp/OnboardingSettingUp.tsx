import { useMemo } from "react";
import clsx from "clsx";

import ConfiguringMiningPool from "./ConfiguringMiningPool";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import { statuses } from "@/shared/constants/statuses";

interface OnboardingSettingUpProps {
  isSetupDone: boolean;
  poolStatus: keyof typeof statuses;
  onClickContinue: () => void;
  onClickReconfigure: () => void;
  onClickRetry: () => void;
}

const OnboardingSettingUp = ({
  isSetupDone,
  poolStatus,
  onClickContinue,
  onClickReconfigure,
  onClickRetry,
}: OnboardingSettingUpProps) => {
  const isLoading = useMemo(() => poolStatus === statuses.fetch || poolStatus === statuses.pending, [poolStatus]);

  const isError = useMemo(() => poolStatus === statuses.error, [poolStatus]);

  const title = useMemo(() => {
    if (isLoading) return "Configuring your miner";
    if (isError) return "There was an issue setting up your miner.";
    return "Your miner is ready";
  }, [isError, isLoading]);

  const subtitle = useMemo(() => {
    if (isLoading) {
      return "This may take a few minutes. Please do not close this window.";
    }
    if (isError) {
      return "View the details below and fix them to continue with setup.";
    }
    return "Continue to your dashboard to view and manage your miner.";
  }, [isError, isLoading]);

  return (
    <>
      <Header title={title} titleSize="text-heading-300" subtitle={subtitle} subtitleSize="text-300" className="mb-3" />
      <ConfiguringMiningPool status={poolStatus} onClickRetry={onClickRetry} onClickReconfigure={onClickReconfigure} />
      <div className="flex justify-end">
        <Button
          variant={variants.primary}
          size={sizes.base}
          text={"Continue"}
          className={clsx(
            "mt-6 transition-opacity duration-200 ease-in-out",
            {
              "animate-[fade-in_.31s_ease-in-out] opacity-100": isSetupDone,
            },
            {
              "cursor-auto opacity-0 hover:opacity-0!": !isSetupDone,
            },
          )}
          onClick={isSetupDone ? onClickContinue : undefined}
          testId="continue-to-dashboard-button"
        />
      </div>
    </>
  );
};

export default OnboardingSettingUp;
