import { useState } from "react";
import { action } from "storybook/actions";
import PoolSelectionModal from "./PoolSelectionModal";

export default {
  title: "Proto Fleet/Fleet Management/PoolSelectionModal",
  component: PoolSelectionModal,
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
      <PoolSelectionModal
        open={open}
        onDismiss={() => {
          action("onDismiss")();
          setOpen(false);
        }}
        onSave={(selectedPoolId, poolData) => {
          action("onSave")({ selectedPoolId, poolData });
          setOpen(false);
        }}
      />
    </>
  );
};
