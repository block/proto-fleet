import { useState } from "react";
import { GlobalActionsWidget } from "./GlobalActionsWidget";
import type { ErrorProps } from "@/protoOS/api/apiResponseTypes";
import { useDownloadLogs } from "@/protoOS/api/hooks/useDownloadLogs";
import { useLocateSystem } from "@/protoOS/api/hooks/useLocateSystem";
import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";
import { PopoverProvider } from "@/shared/components/Popover";

const GlobalActionsWidgetWrapper = () => {
  const { locateSystem } = useLocateSystem();
  const { downloadLogs } = useDownloadLogs();
  const [error, setError] = useState<ErrorProps | null>(null);

  const handleBlinkLEDs = () => {
    locateSystem({
      ledOnTime: 30,
      onError: (err) => setError(err),
    });
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
      {error && (
        <Dialog
          icon={<Alert className="text-text-critical" />}
          title="Error"
          subtitle={error.error?.message || "An error occurred"}
          show={!!error}
          buttons={[
            {
              text: "Close",
              onClick: () => setError(null),
              variant: variants.primary,
            },
          ]}
        />
      )}
    </PopoverProvider>
  );
};

export default GlobalActionsWidgetWrapper;
