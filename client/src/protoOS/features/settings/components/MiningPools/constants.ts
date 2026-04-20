import { STATUSES, ToastStatusType } from "@/shared/features/toaster";

type StatusMessageType = {
  [key in ToastStatusType]: string;
};

export const STATUS_MESSAGES: StatusMessageType = {
  [STATUSES.success]: "Saved",
  [STATUSES.loading]: "Saving changes",
  [STATUSES.queued]: "Saving changes",
  [STATUSES.error]: "Your changes were not saved",
} as const;
