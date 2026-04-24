import { useEffect } from "react";
import { defaultTtl, STATUSES } from "../../constants";
import { type ToastType } from "../../types";
import { Alert, Success } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import ProgressCircular from "@/shared/components/ProgressCircular";

type GroupedToastProps = Omit<ToastType, "id"> & {
  onClose: () => void;
  ttl?: number | false;
};

const GroupedToast = ({ message, onClose, status, progress, actions, ttl = defaultTtl }: GroupedToastProps) => {
  // Only auto-dismiss success toasts, keep error toasts visible for user attention
  useEffect(() => {
    if (status !== STATUSES.success) return;

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
    <div className="space-x-4 bg-surface-elevated-base py-2" data-testid="toast">
      <div className="flex grow items-center space-x-4">
        {icon}
        <div className="flex flex-1 flex-col">
          <div className="text-emphasis-300 text-text-primary">{message}</div>
          {progress !== undefined ? <div className="text-200 text-text-primary-70">{progress}% complete</div> : null}
          {status === STATUSES.queued ? <div className="text-200 text-text-primary-70">Queued</div> : null}
        </div>
        {actions && actions.length > 0 ? (
          <div className="shrink-0">
            {actions.map((action, i) => (
              <Button
                key={i}
                text={action.label}
                variant={variants.primary}
                size={sizes.compact}
                onClick={action.onClick}
                testId={`toast-action-${action.label.toLowerCase().replace(/\s+/g, "-")}`}
              />
            ))}
          </div>
        ) : null}
      </div>
    </div>
  );
};

export default GroupedToast;
