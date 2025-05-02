import clsx from "clsx";
import { ArrowRight, Dismiss } from "@/shared/assets/icons";
import Divider from "@/shared/components/Divider";
import { stepNames } from "@/shared/components/Setup/setupHeader.constants";
import { Step } from "@/shared/components/Setup/setupHeader.types";

type SetupHeaderProps = {
  steps: Step[];
  activeStep: Step;
};

const SetupHeader = ({ steps, activeStep }: SetupHeaderProps) => {
  return (
    <div className="mb-20">
      <div className="flex items-center p-6">
        <Dismiss />
        <div className="mx-auto flex items-center">
          {steps.map((key, index) => (
            <div key={key} className="flex items-center text-text-primary-30">
              <div
                className={clsx(
                  "text-sm font-semibold",
                  activeStep === key && "text-text-emphasis",
                )}
              >
                {stepNames[key]}
              </div>
              {index < Object.values(steps).length - 1 && (
                <ArrowRight width="w-3 mx-2" />
              )}
            </div>
          ))}
        </div>

        <div className="size-5"></div>
      </div>
      <Divider />
    </div>
  );
};

export default SetupHeader;
