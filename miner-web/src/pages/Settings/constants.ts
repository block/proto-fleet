import { STATUSES, ToastStatusType } from "components/Toaster";

type StatusMessageType = {
  [key in ToastStatusType]: string
}

export const STATUS_MESSAGES: StatusMessageType = {
  [STATUSES.success]: "Saved",
  [STATUSES.loading]: "Saving changes",
  [STATUSES.error]: "Your changes were not saved",
} as const;