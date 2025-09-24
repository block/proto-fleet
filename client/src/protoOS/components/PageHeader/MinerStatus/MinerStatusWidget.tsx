import WidgetWrapper from "../WidgetWrapper";
import { type ButtonVariant } from "@/shared/components/Button";
import { MinerStatus as MinerStatusType } from "@/shared/components/MinerStatusModal";
import ProgressCircular from "@/shared/components/ProgressCircular";
import StatusCircle, { variants } from "@/shared/components/StatusCircle/";

interface MinerStatusWidgetProps {
  onClick: () => void;
  status?: MinerStatusType;
  variant?: ButtonVariant;
}

const MinerStatusWidget = ({
  onClick,
  status,
  variant,
}: MinerStatusWidgetProps) => {
  return (
    <WidgetWrapper
      onClick={status ? onClick : undefined}
      variant={variant}
      // workaround for a one off button style
      // TODO: we should either expand our DS or use a button from our DS here
      textColor="text-text-primary"
      borderColor="border-transparent"
    >
      <>
        {!status ? (
          <div className="flex items-center">
            <ProgressCircular
              indeterminate
              dataTestId="miner-status-spinner"
              size={12}
            />
          </div>
        ) : (
          <>
            {status?.circle && (
              <div className="flex items-center">
                <StatusCircle
                  status={status?.circle}
                  variant={variants.simple}
                  width={"w-2"}
                  removeMargin={true}
                />
              </div>
            )}
          </>
        )}
        {status?.summary || "Status"}
      </>
    </WidgetWrapper>
  );
};

export default MinerStatusWidget;
