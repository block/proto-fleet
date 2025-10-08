import GroupedToasterComponent from "./GroupedToaster";
import { STATUSES } from "@/shared/features/toaster";
import { ToastType } from "@/shared/features/toaster";

interface GroupedToasterArgs {
  progress: number;
}

export const GroupedToaster = ({ progress }: GroupedToasterArgs) => {
  const toasts = [
    {
      id: 1,
      message: "Long running action",
      status: STATUSES.loading,
      longRunning: true,
    },
    {
      id: 2,
      message: "Progressing action",
      status: STATUSES.loading,
      longRunning: true,
      progress: progress,
    },
    {
      id: 3,
      message: "Queued action",
      status: STATUSES.queued,
      longRunning: true,
    },
  ] as ToastType[];

  return <GroupedToasterComponent toasts={toasts} />;
};

export default {
  title: "Shared/Grouped Toaster",
  args: {
    progress: 0,
  },
  argTypes: {
    progress: {
      control: { type: "range", min: 0, max: 100, step: 1 },
    },
  },
};
