import clsx from "clsx";
import { ArrowRight, Dismiss } from "@/shared/assets/icons";
import Divider from "@/shared/components/Divider";
import { steps } from "@/shared/components/Setup/setupHeader.constants";
import { Step } from "@/shared/components/Setup/setupHeader.types";

type SetupHeaderProps = {
  activeStep: Step;
};

const SetupHeader = ({ activeStep }: SetupHeaderProps) => {
  return (
    <>
      <div className="flex  items-center p-6">
        <Dismiss />
        <div className="flex items-center mx-auto">
          {(Object.keys(steps) as Step[]).map((key, index) => (
            <div key={key} className="flex items-center text-text-primary-30">
              <div
                className={clsx(
                  "text-sm font-semibold",
                  activeStep === key && "text-text-emphasis",
                )}
              >
                {steps[key].name}
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
    </>
  );
};

export default SetupHeader;
