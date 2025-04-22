import { useState } from "react";

import { clearToasts, pushToast, updateToast } from "../../ToastsObserver";
import ToasterComponent from "./Toaster";
import Button, { sizes, variants } from "@/shared/components/Button";

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
      status: "success",
    });

    setLastToastId(id);
  };

  const updateLastToast = () => {
    if (lastToastId == null) {
      return;
    }

    updateToast(lastToastId, {
      message: `Toast #${lastToastId} has been updated`,
      status: "error",
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
      <div className="fixed right-4 bottom-4 z-20">
        <ToasterComponent />
      </div>
      <div className="absolute flex h-full w-full items-center justify-center gap-3">
        <Button onClick={addToast} size={sizes.base} variant={variants.primary}>
          Add Toast
        </Button>
        <Button
          disabled={lastToastId == null}
          onClick={updateLastToast}
          size={sizes.base}
          variant={variants.primary}
        >
          Update Last Toast
        </Button>
        <Button
          disabled={lastToastId == null}
          onClick={clearAllToasts}
          size={sizes.base}
          variant={variants.primary}
        >
          Clear All Toasts
        </Button>
      </div>
    </div>
  );
};

export default {
  title: "Components (Shared)/Toaster",
};
