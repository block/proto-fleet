import { useState } from "react";

import Button, { sizes, variants } from "components/Button";
import ToasterComponent from "./Toaster";
import { clearToasts, pushToast, updateToast } from "./ToastsObserver";

let counter = 1;
const getCount = () => {
  const count = counter;
  counter++;
  return count;
}

export const Toaster = () => {
  const [lastToastId, setLastToastId] = useState<number | null>(null);
  const addToast = () => {
    const id = pushToast({
      message: `This is toast #${getCount()}`,
      status: "success"
    });

    setLastToastId(id);
  } 

  const updateLastToast = () => {
    if (lastToastId == null) {
      return;
    }

    updateToast(lastToastId, {
      message: `Toast #${lastToastId} has been updated`,
      status: "error"
    });
  }

  const clearAllToasts = () => {
    if (lastToastId == null) {
      return;
    }

    clearToasts();
  }


  return (
    <div>
      <div className="z-20 fixed bottom-4 right-4">
        <ToasterComponent />
      </div>
      <div className="absolute w-full h-full flex items-center justify-center gap-3">
        <Button 
          onClick={addToast} 
          size={sizes.base} 
          variant={variants.primary}
        >
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
  )
};

export default {
  title: "Components/Toaster",
};
