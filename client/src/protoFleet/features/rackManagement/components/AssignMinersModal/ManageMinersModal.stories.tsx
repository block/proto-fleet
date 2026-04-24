import { useState } from "react";
import { action } from "storybook/actions";
import ManageMinersModal from "./ManageMinersModal";

export default {
  title: "Proto Fleet/Rack Management/ManageMinersModal",
  component: ManageMinersModal,
};

export const Default = () => {
  const [show, setShow] = useState(true);

  return (
    <>
      {!show ? (
        <div className="flex h-screen items-center justify-center">
          <button onClick={() => setShow(true)} className="bg-emphasis-300 rounded-lg px-4 py-2 text-surface-base">
            Show Modal
          </button>
        </div>
      ) : null}
      <ManageMinersModal
        show={show}
        currentRackMiners={["miner-001", "miner-002"]}
        currentRackLabel="Rack A-01"
        maxSlots={12}
        onDismiss={() => {
          action("onDismiss")();
          setShow(false);
        }}
        onConfirm={(selectedIds, allSelected, filter) => {
          action("onConfirm")({ selectedIds, allSelected, filter });
          setShow(false);
        }}
      />
    </>
  );
};
