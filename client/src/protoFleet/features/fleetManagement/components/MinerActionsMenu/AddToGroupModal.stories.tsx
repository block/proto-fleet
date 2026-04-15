import { useState } from "react";
import { action } from "storybook/actions";
import AddToGroupModal from "./AddToGroupModal";

export default {
  title: "Proto Fleet/Fleet Management/AddToGroupModal",
  component: AddToGroupModal,
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
      <AddToGroupModal
        open={open}
        selectedMiners={["miner-1", "miner-2", "miner-3"]}
        selectionMode="subset"
        displayCount={3}
        onDismiss={() => {
          action("onDismiss")();
          setOpen(false);
        }}
      />
    </>
  );
};

export const SingleMiner = () => {
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
      <AddToGroupModal
        open={open}
        selectedMiners={["miner-1"]}
        selectionMode="subset"
        displayCount={1}
        onDismiss={() => {
          action("onDismiss")();
          setOpen(false);
        }}
      />
    </>
  );
};

export const AllMinersSelected = () => {
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
      <AddToGroupModal
        open={open}
        selectedMiners={[]}
        selectionMode="all"
        displayCount={50}
        onDismiss={() => {
          action("onDismiss")();
          setOpen(false);
        }}
      />
    </>
  );
};
