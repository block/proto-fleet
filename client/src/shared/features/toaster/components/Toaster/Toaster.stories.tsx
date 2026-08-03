import { useState } from "react";

import { clearToasts, pushToast, updateToast } from "../../ToastsObserver";
import ToasterComponent from "./Toaster";
import Button, { sizes, variants } from "@/shared/components/Button";
import { STATUSES } from "@/shared/features/toaster";

let counter = 1;
const getCount = () => {
  const count = counter;
  counter++;
  return count;
};

export const Toaster = () => {
  const [lastToastId, setLastToastId] = useState<number | null>(null);
  const addToast = () => {
    const id = pushToast({
      message: `This is toast #${getCount()}`,
      status: STATUSES.success,
    });

    setLastToastId(id);
  };

  const addLongRunningToast = (queued: boolean) => {
    const id = pushToast({
      message: queued ? `This is a queued action #${getCount()}` : `This is a long running action #${getCount()}`,
      status: queued ? STATUSES.queued : STATUSES.loading,
      longRunning: true,
    });

    setLastToastId(id);
  };

  const addProgressingToast = () => {
    let progress = 0;
    const id = pushToast({
      message: `This is a progressing action #${getCount()}`,
      status: STATUSES.loading,
      longRunning: true,
      progress: 0,
    });

    setLastToastId(id);

    const intervalId = setInterval(() => {
      progress += 10;
      if (progress > 100) {
        clearInterval(intervalId);
      } else {
        updateToast(id, {
          progress: progress,
        });
      }
    }, 500);
  };

  const updateLastToast = () => {
    if (lastToastId == null) {
      return;
    }

    updateToast(lastToastId, {
      message: `Toast #${lastToastId} has been updated`,
      status: STATUSES.error,
    });
  };

  const clearAllToasts = () => {
    if (lastToastId == null) {
      return;
    }

    clearToasts();
  };

  return (
    <div>
      <div className="fixed right-4 bottom-4 z-20 phone:right-2 phone:bottom-2">
        <ToasterComponent />
      </div>
      <div className="absolute">
        <div className="flex h-full w-full flex-wrap items-center justify-center gap-3">
          <Button onClick={addToast} size={sizes.base} variant={variants.primary}>
            Add Toast
          </Button>
          <Button onClick={() => addLongRunningToast(false)} size={sizes.base} variant={variants.primary}>
            Add action Toast
          </Button>
          <Button onClick={addProgressingToast} size={sizes.base} variant={variants.primary}>
            Add progressing Toast
          </Button>
          <Button onClick={() => addLongRunningToast(true)} size={sizes.base} variant={variants.primary}>
            Add queued Toast
          </Button>
          <Button disabled={lastToastId == null} onClick={updateLastToast} size={sizes.base} variant={variants.primary}>
            Update Last Toast
          </Button>
          <Button disabled={lastToastId == null} onClick={clearAllToasts} size={sizes.base} variant={variants.primary}>
            Clear All Toasts
          </Button>
        </div>
      </div>
    </div>
  );
};

export default {
  title: "Shared/Toaster",
};
