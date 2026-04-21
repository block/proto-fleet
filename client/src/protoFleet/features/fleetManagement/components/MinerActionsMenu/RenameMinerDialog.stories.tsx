import { useState } from "react";
import { action } from "storybook/actions";
import RenameMinerDialog from "./RenameMinerDialog";

export default {
  title: "Proto Fleet/Fleet Management/RenameMinerDialog",
  component: RenameMinerDialog,
};

export const Default = () => {
  const [open, setOpen] = useState(true);

  return (
    <>
      {!open && (
        <div className="flex h-screen items-center justify-center">
          <button onClick={() => setOpen(true)} className="bg-emphasis-300 rounded-lg px-4 py-2 text-surface-base">
            Show Modal
          </button>
        </div>
      )}
      <RenameMinerDialog
        open={open}
        deviceIdentifier="device-abc-123"
        onConfirm={(name) => {
          action("onConfirm")(name);
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
