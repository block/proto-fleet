import { useState } from "react";
import { action } from "storybook/actions";
import CreateApiKeyModal from "./CreateApiKeyModal";

export default {
  title: "Proto Fleet/Settings/CreateApiKeyModal",
  component: CreateApiKeyModal,
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
      <CreateApiKeyModal
        open={open}
        onSuccess={() => action("onSuccess")()}
        onDismiss={() => {
          action("onDismiss")();
          setOpen(false);
        }}
      />
    </>
  );
};
