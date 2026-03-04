import { useEffect, useState } from "react";
import { GlobalActionsWidget } from "./GlobalActionsWidget";
import type { ErrorProps } from "@/protoOS/api/apiResponseTypes";
import { useDownloadLogs } from "@/protoOS/api/hooks/useDownloadLogs";
import { useLocateSystem } from "@/protoOS/api/hooks/useLocateSystem";
import {
  AUTH_ACTIONS,
  useAccessToken,
  useDismissedLoginModal,
  usePausedAuthAction,
  useSetDismissedLoginModal,
  useSetPausedAuthAction,
} from "@/protoOS/store";
import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";
import { PopoverProvider } from "@/shared/components/Popover";

const GlobalActionsWidgetWrapper = () => {
  const { locateSystem } = useLocateSystem();
  const { downloadLogs } = useDownloadLogs();
  const [error, setError] = useState<ErrorProps | null>(null);

  const dismissedLoginModal = useDismissedLoginModal();
  const setDismissedLoginModal = useSetDismissedLoginModal();
  const pausedAuthAction = usePausedAuthAction();
  const setPausedAuthAction = useSetPausedAuthAction();

  const { checkAccess, hasAccess } = useAccessToken(pausedAuthAction === AUTH_ACTIONS.locate && !dismissedLoginModal);

  // After successful login, retry the locate request
  useEffect(() => {
    if (hasAccess && pausedAuthAction === AUTH_ACTIONS.locate) {
      setPausedAuthAction(null);
      locateSystem({
        ledOnTime: 30,
        onError: (err) => setError(err),
      });
    }
  }, [hasAccess, pausedAuthAction, setPausedAuthAction, locateSystem]);

  // Clean up paused action if user dismissed the login modal
  useEffect(() => {
    if (dismissedLoginModal) {
      setPausedAuthAction(null);
      setDismissedLoginModal(false);
    }
  }, [dismissedLoginModal, setDismissedLoginModal, setPausedAuthAction]);

  const handleBlinkLEDs = () => {
    setPausedAuthAction(AUTH_ACTIONS.locate);
    checkAccess();
  };

  const handleDownloadLogs = async () => {
    try {
      await downloadLogs();
    } catch (_err) {
      setError({ status: 500, error: { message: "Failed to download logs" } });
    }
  };

  return (
    <PopoverProvider>
      <GlobalActionsWidget onBlinkLEDs={handleBlinkLEDs} onDownloadLogs={handleDownloadLogs} />
      <Dialog
        open={!!error}
        icon={<Alert className="text-text-critical" />}
        title="Error"
        subtitle={error?.error?.message || "An error occurred"}
        onDismiss={() => setError(null)}
        buttons={[
          {
            text: "Close",
            onClick: () => setError(null),
            variant: variants.primary,
          },
        ]}
      />
    </PopoverProvider>
  );
};

export default GlobalActionsWidgetWrapper;
