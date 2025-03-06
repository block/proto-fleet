import { type ACTIONS, type STATUSES } from "./constants";

export type ToastStatusType = (typeof STATUSES)[keyof typeof STATUSES];
export type ToasterActionType = (typeof ACTIONS)[keyof typeof ACTIONS];

// Toast type that Toaster useState expects
export type ToastType = {
  message: string;
  status: ToastStatusType;
  id: number;
};

// Props interface Toast component accepts
export type ToastProps = Omit<ToastType, "id"> & {
  onClose: () => void;
  index?: number;
  numToasts?: number;
  ttl?: number;
};
