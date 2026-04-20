import { ACTIONS } from "./constants";
import { ToastType } from "./types";

type UpdateProps = {
  action: typeof ACTIONS.update;
  toast: Partial<ToastType>;
};

type RemoveProps = {
  action: typeof ACTIONS.remove;
  id: number;
};

type PushProps = {
  action: typeof ACTIONS.push;
  toast: ToastType;
};

type ClearProps = {
  action: typeof ACTIONS.clear;
};

type NotifyProps = UpdateProps | RemoveProps | PushProps | ClearProps;
type cbType = (data: NotifyProps) => any;

type ToastTypeWithoutId = Omit<ToastType, "id">;

let counter = 1;
export const getToastId = (): number => {
  const id = counter;
  counter++;
  return id;
};

const observers: cbType[] = [];

const ToastsObserver = Object.freeze({
  notify: (data: NotifyProps) => {
    observers.forEach((observer) => observer(data));
  },
  subscribe: (func: cbType) => {
    observers.push(func);
  },
  unsubscribe: (func: cbType) => {
    [...observers].forEach((observer, idx) => {
      if (observer === func) {
        observers.splice(idx, 1);
      }
    });
  },
});

const pushToast = (toast: ToastTypeWithoutId) => {
  const id = getToastId();
  ToastsObserver.notify({
    action: "push",
    toast: { ...toast, id },
  });

  return id;
};

const updateToast = (id: number, toast: Partial<ToastTypeWithoutId>) => {
  ToastsObserver.notify({
    action: ACTIONS.update,
    toast: { ...toast, id },
  });

  return id;
};

const removeToast = (id: number | null) => {
  if (!id) {
    return;
  }

  ToastsObserver.notify({
    action: ACTIONS.remove,
    id,
  });
};

const clearToasts = () => {
  ToastsObserver.notify({
    action: ACTIONS.clear,
  });
};

export default ToastsObserver;
export { pushToast, updateToast, removeToast, clearToasts };
