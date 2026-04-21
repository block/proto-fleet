import { useState } from "react";
import { action } from "storybook/actions";
import FirmwareUpdateModal from "./FirmwareUpdateModal";

export default {
  title: "Proto Fleet/Fleet Management/FirmwareUpdateModal",
  component: FirmwareUpdateModal,
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
      <FirmwareUpdateModal
        open={open}
        onConfirm={(firmwareFileId) => {
          action("onConfirm")(firmwareFileId);
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
