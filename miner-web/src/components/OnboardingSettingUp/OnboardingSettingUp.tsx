import clsx from "clsx";

import Button, { sizes, variants } from "components/Button";
import Header from "components/Header";

import { ArrowRight } from "icons";

import { type statuses } from "./constants";
import Item from "./Item";

interface OnboardingSettingUpProps {
  isSetupDone: boolean,
  poolStatus: keyof typeof statuses,
  onClickContinue: () => void,
  onClickRetry: () => void,
}

const OnboardingSettingUp = ({
  isSetupDone,
  poolStatus,
  onClickContinue,
  onClickRetry,
}: OnboardingSettingUpProps) => {
  return (
    <>
      <Header
        title="We’re setting up your miner"
        titleSize="text-heading-300"
        subtitle="This may take a few minutes. Please do not close this window."
        subtitleSize="text-300"
        className="mb-3"
      />
      <Item
        status={poolStatus}
        text="mining pools"
        onClickRetry={onClickRetry}
          divider={false}
      />
      <div className="flex justify-end">
        <Button
          variant={variants.accent}
          size={sizes.base}
          text="Continue to dashboard"
          className={clsx(
            "mt-6 transition-opacity ease-in-out duration-200",
            {
              "opacity-100 animate-[fade-in_.31s_ease-in-out]": isSetupDone,
            },
            {
              "opacity-0 hover:opacity-0 cursor-auto": !isSetupDone,
            }
          )}
          suffixIcon={<ArrowRight />}
          onClick={isSetupDone ? onClickContinue : undefined}
          testId="continue-to-dashboard-button"
        />
      </div>
    </>
  );
};

export default OnboardingSettingUp;
