import { AnimatePresence, motion } from "motion/react";
import { useEffect, useState } from "react";

import { ACTIONS } from "./constants";
import Toast from "./Toast";
import ToastsObserver, { removeToast } from "./ToastsObserver";
import { type ToastType } from "./types";

const Toaster = () => {
  const [toasts, setToasts] = useState<ToastType[]>([]);

  useEffect(() => {
    ToastsObserver.subscribe((data) => {
      if (data.action == ACTIONS.push) {
        setToasts((prev) => [...prev, data.toast]);

      } else if (data.action == ACTIONS.remove) {
        setToasts((prev) => prev.filter((t) => t.id !== data.id));

      } else if (data.action == ACTIONS.update) {
        setToasts((prev) => {
          const index = prev.findIndex((t) => t.id == data.toast.id);
          if (index == undefined) {
            return prev;
          }

          const clone = [...prev];
          clone.splice(index, 1, data.toast);
          return clone;
        });
        
      } else if (data.action == ACTIONS.clear) {
        setToasts([]);
      }
    });
  }, []);

  return (
    <motion.div whileHover="hover" className="group">
      <AnimatePresence>
        {toasts.map(({ message, status, id }: ToastType, idx) => (
          <Toast 
            key={id}
            message={message}
            onClose={() => removeToast(id)}
            status={status}
            index={idx}
            numToasts={toasts.length}
          />
        ))}
      </AnimatePresence>
    </motion.div>
  )
}

export default Toaster;