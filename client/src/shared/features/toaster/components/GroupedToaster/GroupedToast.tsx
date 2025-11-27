import { useEffect } from "react";
import { defaultTtl, STATUSES } from "../../constants";
import { type ToastType } from "../../types";
import { Alert, Success } from "@/shared/assets/icons";
import ProgressCircular from "@/shared/components/ProgressCircular";

type GroupedToastProps = Omit<ToastType, "id"> & {
  onClose: () => void;
  ttl?: number | false;
};

const GroupedToast = ({ message, onClose, status, progress, ttl = defaultTtl }: GroupedToastProps) => {
  useEffect(() => {
    if (status !== STATUSES.success && status !== STATUSES.error) return;

    if (ttl !== false) {
      const toID = setTimeout(onClose, ttl);
      return () => {
        clearTimeout(toID);
      };
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ttl, status]);

  let icon = <ProgressCircular indeterminate dataTestId="loading-progress-circular" />;
  if (status === STATUSES.success) icon = <Success className="text-intent-success-fill" />;
  else if (status === STATUSES.error) icon = <Alert className="text-intent-warning-fill" />;
  else if (progress !== undefined)
    icon = <ProgressCircular value={progress} dataTestId="progressing-progress-circular" />;
  else if (status === STATUSES.queued) icon = <ProgressCircular dataTestId="queued-progress-circular" />;

  return (
    <div className="space-x-4 bg-surface-elevated-base py-2">
      <div className="flex grow items-center space-x-4">
        {icon}
        <div className="flex flex-col">
          <div className="text-emphasis-300 text-text-primary">{message}</div>
          {progress !== undefined && <div className="text-200 text-text-primary-70">{progress}% complete</div>}
          {status === STATUSES.queued && <div className="text-200 text-text-primary-70">Queued</div>}
        </div>
      </div>
    </div>
  );
};

export default GroupedToast;
