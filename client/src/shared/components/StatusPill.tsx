import Button, { variants as buttonVariants } from "@/shared/components/Button";
import ProgressCircular from "@/shared/components/ProgressCircular";
import StatusCircle, { statuses, variants } from "@/shared/components/StatusCircle/";

interface StatusPillProps {
  onClick: () => void;
  ariaLabel?: string;
  summary?: string;
  circle?: keyof typeof statuses;
}

const StatusPill = ({ onClick, summary, circle, ariaLabel }: StatusPillProps) => {
  return (
    <Button
      onClick={summary ? onClick : undefined}
      variant={buttonVariants.secondary}
      textColor="text-text-primary"
      borderColor="border-transparent"
      size="compact"
      className="flex h-7 items-center rounded-2xl px-2 py-1 whitespace-nowrap"
      ariaLabel={ariaLabel}
      ariaHasPopup="dialog"
    >
      <div className="flex gap-2">
        {!summary ? (
          <div className="flex items-center">
            <ProgressCircular indeterminate dataTestId="miner-status-spinner" size={12} />
          </div>
        ) : (
          <>
            {circle ? (
              <div className="flex items-center">
                <StatusCircle status={circle} variant={variants.simple} width={"w-2"} removeMargin={true} />
              </div>
            ) : null}
          </>
        )}
        {summary || "Status"}
      </div>
    </Button>
  );
};

export default StatusPill;
