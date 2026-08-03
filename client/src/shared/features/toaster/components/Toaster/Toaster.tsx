import { AnimatePresence, motion } from "motion/react";
import { useEffect, useMemo, useState } from "react";

import { ACTIONS, STATUSES } from "../../constants";
import ToastsObserver, { removeToast } from "../../ToastsObserver";
import { type ToastType } from "../../types";
import GroupedToaster from "../GroupedToaster";
import Toast from "../Toast";

const Toaster = () => {
  const [toasts, setToasts] = useState<ToastType[]>([]);

  const basicToasts = useMemo(() => {
    return toasts.filter(
      (toast) => !toast.longRunning && (toast.status === STATUSES.success || toast.status === STATUSES.error),
    );
  }, [toasts]);

  const actionToasts = useMemo(() => {
    return toasts.filter((toast) => toast.longRunning === true || toast.status === STATUSES.loading);
  }, [toasts]);

  useEffect(() => {
    ToastsObserver.subscribe((data) => {
      if (data.action == ACTIONS.push) {
        setToasts((prev) => [...prev, data.toast]);
      } else if (data.action == ACTIONS.remove) {
        setToasts((prev) => prev.filter((t) => t.id !== data.id));
      } else if (data.action == ACTIONS.update) {
        setToasts((prev) => {
          const index = prev.findIndex((t) => t.id === data.toast.id);
          if (index === -1) {
            return prev;
          }

          // perform patch of original toast
          const updatedToast = { ...prev[index], ...data.toast };
          const clone = [...prev];
          clone.splice(index, 1, updatedToast);
          return clone;
        });
      } else if (data.action == ACTIONS.clear) {
        setToasts([]);
      }
    });
  }, []);

  const handleToastClose = (id: number, customOnClose?: () => void) => {
    removeToast(id);
    customOnClose?.();
  };

  return (
    <>
      <motion.div whileHover="hover" className="group absolute -top-5 right-0">
        <AnimatePresence>
          {basicToasts.map(({ message, status, id, ttl, onClose }: ToastType, idx) => (
            <Toast
              key={id}
              message={message}
              onClose={() => handleToastClose(id, onClose)}
              status={status}
              index={idx}
              numToasts={basicToasts.length}
              ttl={ttl}
            />
          ))}
        </AnimatePresence>
      </motion.div>
      <GroupedToaster toasts={actionToasts} />
    </>
  );
};

export default Toaster;
