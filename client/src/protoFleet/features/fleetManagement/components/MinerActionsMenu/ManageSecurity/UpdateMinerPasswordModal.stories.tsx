import { useState } from "react";
import { action } from "storybook/actions";
import UpdateMinerPasswordModal from "./UpdateMinerPasswordModal";

export default {
  title: "Proto Fleet/Fleet Management/UpdateMinerPasswordModal",
  component: UpdateMinerPasswordModal,
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
      <UpdateMinerPasswordModal
        open={open}
        hasThirdPartyMiners={false}
        onConfirm={(currentPassword, newPassword) => {
          action("onConfirm")({ currentPassword, newPassword });
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

export const WithThirdPartyMiners = () => {
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
      <UpdateMinerPasswordModal
        open={open}
        hasThirdPartyMiners={true}
        onConfirm={(currentPassword, newPassword) => {
          action("onConfirm")({ currentPassword, newPassword });
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
