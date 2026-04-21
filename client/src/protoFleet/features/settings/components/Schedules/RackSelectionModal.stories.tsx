import { useState } from "react";
import { action } from "storybook/actions";
import RackSelectionModal from "./RackSelectionModal";

export default {
  title: "Proto Fleet/Settings/Schedules/RackSelectionModal",
  component: RackSelectionModal,
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
      <RackSelectionModal
        open={open}
        selectedRackIds={[]}
        onDismiss={() => {
          action("onDismiss")();
          setOpen(false);
        }}
        onSave={(rackIds) => {
          action("onSave")(rackIds);
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
      {!open && (
        <div className="flex h-screen items-center justify-center">
          <button onClick={() => setOpen(true)} className="bg-emphasis-300 rounded-lg px-4 py-2 text-surface-base">
            Show Modal
          </button>
        </div>
      )}
      <RackSelectionModal
        open={open}
        selectedRackIds={["1", "3"]}
        onDismiss={() => {
          action("onDismiss")();
          setOpen(false);
        }}
        onSave={(rackIds) => {
          action("onSave")(rackIds);
          setOpen(false);
        }}
      />
    </>
  );
};
