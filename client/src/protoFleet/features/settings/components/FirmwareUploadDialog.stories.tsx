import { useState } from "react";
import { action } from "storybook/actions";
import FirmwareUploadDialog from "./FirmwareUploadDialog";

export default {
  title: "Proto Fleet/Settings/FirmwareUploadDialog",
  component: FirmwareUploadDialog,
};

export const Default = () => {
  const [open, setOpen] = useState(true);

  return (
    <>
      {!open ? (
        <div className="flex h-screen items-center justify-center">
          <button onClick={() => setOpen(true)} className="bg-emphasis-300 rounded-lg px-4 py-2 text-surface-base">
            Show Modal
          </button>
        </div>
      ) : null}
      <FirmwareUploadDialog
        open={open}
        onSuccess={() => {
          action("onSuccess")();
          setOpen(false);
        }}
        onDismiss={() => {
          action("onDismiss")();
          setOpen(false);
        }}
      />
    </>
  );
};
