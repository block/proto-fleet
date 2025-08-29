import WidgetWrapper from "../WidgetWrapper";
import { MinerStatus as MinerStatusType } from "@/shared/components/MinerStatusModal";
import ProgressCircular from "@/shared/components/ProgressCircular";
import StatusCircle, { variants } from "@/shared/components/StatusCircle/";

interface MinerStatusWidgetProps {
  onClick: () => void;
  status?: MinerStatusType;
}

const MinerStatusWidget = ({ onClick, status }: MinerStatusWidgetProps) => {
  return (
    <WidgetWrapper onClick={status ? onClick : undefined}>
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
