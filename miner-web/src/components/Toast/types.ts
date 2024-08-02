import { type toastTypes } from "./constants";

export type ToastType = typeof toastTypes[keyof typeof toastTypes];
