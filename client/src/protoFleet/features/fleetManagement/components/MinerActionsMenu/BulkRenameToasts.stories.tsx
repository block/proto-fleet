import { useEffect } from "react";

import {
  getBulkRenameFailureMessage,
  getBulkRenameLoadingMessage,
  getBulkRenameRequestFailureMessage,
  getBulkRenameSuccessMessage,
} from "@/protoFleet/features/fleetManagement/components/MinerActionsMenu/bulkRenameToastMessages";
import Button, { sizes, variants } from "@/shared/components/Button";
import {
  clearToasts,
  pushToast,
  removeToast,
  STATUSES,
  Toaster as ToasterComponent,
  updateToast,
} from "@/shared/features/toaster";

interface BulkRenameToastStoryProps {
  failedCount: number;
  renamedCount: number;
  selectionCount: number;
  unchangedCount: number;
  requestFailed?: boolean;
}

const playBulkRenameToastScenario = ({
  failedCount,
  renamedCount,
  selectionCount,
  unchangedCount,
  requestFailed = false,
}: BulkRenameToastStoryProps) => {
  clearToasts();

  const toastId = pushToast({
    message: getBulkRenameLoadingMessage(selectionCount),
    status: STATUSES.loading,
    longRunning: true,
  });

  if (requestFailed) {
    updateToast(toastId, {
      message: getBulkRenameRequestFailureMessage(selectionCount),
      status: STATUSES.error,
    });
    return;
  }

  if (renamedCount > 0 || unchangedCount > 0) {
    updateToast(toastId, {
      message: getBulkRenameSuccessMessage(renamedCount, unchangedCount),
      status: STATUSES.success,
    });
  } else {
    removeToast(toastId);
  }

  if (failedCount > 0) {
    pushToast({
      message: getBulkRenameFailureMessage(failedCount),
      status: STATUSES.error,
      longRunning: true,
    });
  }
};

const StoryLayout = (props: BulkRenameToastStoryProps) => {
  useEffect(() => {
    playBulkRenameToastScenario(props);

    return () => {
      clearToasts();
    };
  }, [props]);

  return (
    <div className="flex min-h-screen items-center justify-center bg-surface-base">
      <div className="flex flex-col items-center gap-4">
        <Button onClick={() => playBulkRenameToastScenario(props)} size={sizes.base} variant={variants.primary}>
          Replay rename toasts
        </Button>
        <p className="max-w-[560px] text-center text-300 text-text-primary-70">
          This story replays the exact toast copy used by bulk rename for the selected result combination.
        </p>
      </div>
      <div className="fixed right-4 bottom-4 z-20 phone:right-2 phone:bottom-2">
        <ToasterComponent />
      </div>
    </div>
  );
};

export const RenamedOnly = () => <StoryLayout selectionCount={4} renamedCount={4} unchangedCount={0} failedCount={0} />;

export const UnchangedOnly = () => (
  <StoryLayout selectionCount={3} renamedCount={0} unchangedCount={3} failedCount={0} />
);

export const RenamedAndUnchanged = () => (
  <StoryLayout selectionCount={6} renamedCount={4} unchangedCount={2} failedCount={0} />
);

export const RenamedUnchangedAndFailed = () => (
  <StoryLayout selectionCount={7} renamedCount={4} unchangedCount={2} failedCount={1} />
);

export const FailedOnly = () => <StoryLayout selectionCount={2} renamedCount={0} unchangedCount={0} failedCount={2} />;

export const RequestFailure = () => (
  <StoryLayout selectionCount={5} renamedCount={0} unchangedCount={0} failedCount={0} requestFailed />
);

export default {
  title: "Proto Fleet/Fleet Management/Bulk Rename/Toasts",
};
