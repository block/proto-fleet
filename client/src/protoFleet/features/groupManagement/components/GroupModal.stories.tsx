import { useState } from "react";
import { action } from "storybook/actions";
import GroupModal from "./GroupModal";

export default {
  title: "Proto Fleet/Group Management/GroupModal",
  component: GroupModal,
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
      <GroupModal
        show={show}
        onDismiss={() => {
          action("onDismiss")();
          setShow(false);
        }}
        onSuccess={() => action("onSuccess")()}
      />
    </>
  );
};
