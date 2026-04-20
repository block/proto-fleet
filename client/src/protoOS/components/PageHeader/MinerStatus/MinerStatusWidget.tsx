import WidgetWrapper from "../WidgetWrapper";
import { variants as buttonVariants } from "@/shared/components/Button";
import ProgressCircular from "@/shared/components/ProgressCircular";
import StatusCircle, { statuses, variants } from "@/shared/components/StatusCircle/";

interface MinerStatusWidgetProps {
  onClick: () => void;
  summary?: string;
  circle?: keyof typeof statuses;
}

const MinerStatusWidget = ({ onClick, summary, circle }: MinerStatusWidgetProps) => {
  return (
    <WidgetWrapper
      onClick={summary ? onClick : undefined}
      variant={buttonVariants.secondary}
      // workaround for a one off button style
      // TODO: we should either expand our DS or use a button from our DS here
      textColor="text-text-primary"
      borderColor="border-transparent"
    >
      <>
        {!summary ? (
          <div className="flex items-center">
            <ProgressCircular indeterminate dataTestId="miner-status-spinner" size={12} />
          </div>
        ) : (
          <>
            {circle && (
              <div className="flex items-center">
                <StatusCircle status={circle} variant={variants.simple} width={"w-2"} removeMargin={true} />
              </div>
            )}
          </>
        )}
        {summary || "Status"}
      </>
    </WidgetWrapper>
  );
};

export default MinerStatusWidget;
