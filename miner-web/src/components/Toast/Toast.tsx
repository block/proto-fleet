import Spinner from "components/Spinner";

import { Alert, Dismiss, Success } from "icons";
import { iconSizes } from "icons/constants";

import { toastTypes } from "./constants";
import { ToastType } from "./types";

interface ToastProps {
  message: string;
  onClose: () => void;
  type: ToastType;
}

const Toast = ({ message, onClose, type }: ToastProps) => {
  return (
    <div className="flex items-center w-[400px] p-3 space-x-3 rounded-lg shadow-100 bg-surface-base">
      <div className="flex grow space-x-3 items-center">
        {type === toastTypes.loading && <Spinner />}
        {type === toastTypes.success && (
          <Success className="text-intent-success-fill" />
        )}
        {type === toastTypes.error && (
          <Alert className="text-intent-warning-fill" />
        )}
        <div className="text-heading-100 text-text-primary/90">{message}</div>
      </div>
      <button onClick={onClose}>
        <Dismiss className="text-text-primary/30" width={iconSizes.small} />
      </button>
    </div>
  );
};

export default Toast;
