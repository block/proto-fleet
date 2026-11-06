import { poolStatusIsProblem } from "./status";
import ProgressCircular from "@/shared/components/ProgressCircular";

export const PoolStatus = ({ status, name, onDetails }: { status: string; name?: string; onDetails: () => void }) => {
  if (poolStatusIsProblem(status) || ["Verified", "Device configuration", "Container configuration"].includes(status))
    return (
      <button
        type="button"
        className="max-w-full cursor-pointer rounded-sm text-left text-text-primary underline-offset-2 hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-core-primary-fill"
        aria-haspopup="dialog"
        onClick={onDetails}
      >
        {status === "Verified" ? name || status : status}
      </button>
    );
  return (
    <span className="flex items-center gap-2 text-text-primary-70" role="status">
      {status === "Updating…" ? (
        <ProgressCircular indeterminate size={14} dataTestId="pool-configuration-progress" />
      ) : null}
      {status}
    </span>
  );
};
