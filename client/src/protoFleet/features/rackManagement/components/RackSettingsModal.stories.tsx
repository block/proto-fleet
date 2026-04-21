import { useState } from "react";
import { action } from "storybook/actions";
import RackSettingsModal from "./RackSettingsModal";

export default {
  title: "Proto Fleet/Rack Management/RackSettingsModal",
  component: RackSettingsModal,
};

export const CreateNew = () => {
  const [show, setShow] = useState(true);

  return (
    <>
      {!show && (
        <div className="flex h-screen items-center justify-center">
          <button onClick={() => setShow(true)} className="bg-emphasis-300 rounded-lg px-4 py-2 text-surface-base">
            Show Modal
          </button>
        </div>
      )}
      <RackSettingsModal
        show={show}
        existingRacks={[]}
        onDismiss={() => {
          action("onDismiss")();
          setShow(false);
        }}
        onContinue={(formData) => {
          action("onContinue")(formData);
          setShow(false);
        }}
      />
    </>
  );
};
