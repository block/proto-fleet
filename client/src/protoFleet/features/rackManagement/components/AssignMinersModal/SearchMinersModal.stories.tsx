import { useState } from "react";
import { action } from "storybook/actions";
import SearchMinersModal from "./SearchMinersModal";

export default {
  title: "Proto Fleet/Rack Management/SearchMinersModal",
  component: SearchMinersModal,
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
      <SearchMinersModal
        show={show}
        currentRackLabel="Rack A-01"
        onDismiss={() => {
          action("onDismiss")();
          setShow(false);
        }}
        onConfirm={(selectedMinerId) => {
          action("onConfirm")(selectedMinerId);
          setShow(false);
        }}
      />
    </>
  );
};
