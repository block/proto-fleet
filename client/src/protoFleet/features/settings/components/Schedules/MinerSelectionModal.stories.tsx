import { useState } from "react";
import { action } from "storybook/actions";
import MinerSelectionModal from "./MinerSelectionModal";

export default {
  title: "Proto Fleet/Settings/Schedules/MinerSelectionModal",
  component: MinerSelectionModal,
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
      <MinerSelectionModal
        open={open}
        selectedMinerIds={[]}
        onDismiss={() => {
          action("onDismiss")();
          setOpen(false);
        }}
        onSave={(minerIds) => {
          action("onSave")(minerIds);
          setOpen(false);
        }}
      />
    </>
  );
};

export const WithPreselected = () => {
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
      <MinerSelectionModal
        open={open}
        selectedMinerIds={["miner-001", "miner-002"]}
        onDismiss={() => {
          action("onDismiss")();
          setOpen(false);
        }}
        onSave={(minerIds) => {
          action("onSave")(minerIds);
          setOpen(false);
        }}
      />
    </>
  );
};
