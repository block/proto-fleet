import { poolStatusIsProblem } from "./status";
import Button from "@/shared/components/Button";
import ProgressCircular from "@/shared/components/ProgressCircular";

export const PoolStatus = ({ status, name, onDetails }: { status: string; name?: string; onDetails: () => void }) => {
  if (status === "Verified") return <span>{name}</span>;
  if (poolStatusIsProblem(status) || ["Device configuration", "Container configuration"].includes(status))
    return (
      <Button
        variant="textOnly"
        textColor="text-text-primary"
        text={status}
        className="font-normal underline underline-offset-2"
        textOnlyUnderlineOnHover={false}
        onClick={onDetails}
      />
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
