import { type ACTIONS, type STATUSES } from "./constants";

export type ToastStatusType = (typeof STATUSES)[keyof typeof STATUSES];
export type ToasterActionType = (typeof ACTIONS)[keyof typeof ACTIONS];

export type ToastAction = {
  label: string;
  onClick: () => void;
};

// Toast type that Toaster useState expects
export type ToastType = {
  message: string;
  status: ToastStatusType;
  id: number;
  ttl?: number | false;
  longRunning?: boolean;
  progress?: number;
  onClose?: () => void;
  actions?: ToastAction[];
};

// Props interface Toast component accepts
export type ToastProps = Omit<ToastType, "id"> & {
  onClose: () => void;
  index?: number;
  numToasts?: number;
  ttl?: number | false;
};
