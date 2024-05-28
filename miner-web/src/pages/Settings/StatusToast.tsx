import { useMemo } from "react";

import { useKeyDown } from "common/hooks/useKeyDown";

import Toast, { ToastType, toastTypes } from "components/Toast";

interface StatusToastProps {
  onClose: () => void;
  type: ToastType | null;
}

const StatusToast = ({ onClose, type }: StatusToastProps) => {
  useKeyDown({ key: "Escape", onKeyDown: onClose });

  const message = useMemo(() => {
    if (type === toastTypes.loading) {
      return "Saving changes";
    } else if (type === toastTypes.success) {
      return "Saved";
    } else if (type === toastTypes.error) {
      return "Your changes were not saved";
    }
    return "";
  }, [type]);

  return (
    <>
      {type && (
        <div className="fixed right-4 bottom-4 z-10">
          <Toast message={message} onClose={onClose} type={type} />
        </div>
      )}
    </>
  );
};

export default StatusToast;
