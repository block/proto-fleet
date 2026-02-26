import { RefObject } from "react";

import Dialog from "@/shared/components/Dialog";
import { getFileName } from "@/shared/utils/utility";

interface ExportingLogsDialogProps {
  exportLink?: string;
  linkRef?: RefObject<HTMLAnchorElement>;
  open?: boolean;
}

const ExportingLogsDialog = ({ exportLink, linkRef, open }: ExportingLogsDialogProps) => {
  return (
    <>
      <Dialog
        open={open}
        title="Exporting logs"
        preventScroll
        subtitle="Your logs are being exported. This may take a few seconds."
        subtitleSize="text-300"
        loading
        testId="exporting-logs-dialog"
      />
      <a href={exportLink || ""} download={`${getFileName("miner-logs")}`} ref={linkRef} />
    </>
  );
};

export default ExportingLogsDialog;
