import { ACTIONS, STATUSES } from "../Toaster/constants";
import Toaster from "./Toaster";

import ToastsObserver, {
  clearToasts,
  pushToast, 
  removeToast,
  updateToast
} from "./ToastsObserver";

import { 
  ToasterActionType,
  ToastStatusType, 
  ToastType, 
} from "./types";

export { 
  type ToastType,
  type ToastStatusType,
  type ToasterActionType,
  STATUSES, 
  ACTIONS, 
  ToastsObserver,
  pushToast,
  updateToast,
  removeToast,
  clearToasts
};
export default Toaster;
