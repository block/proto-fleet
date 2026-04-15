import { useState } from "react";
import { action } from "storybook/actions";
import AuthenticateMiners from "./AuthenticateMiners";

export default {
  title: "Proto Fleet/Auth/AuthenticateMiners",
  component: AuthenticateMiners,
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
      <AuthenticateMiners
        open={open}
        onClose={() => {
          action("onClose")();
          setOpen(false);
        }}
        onSuccess={() => action("onSuccess")()}
      />
    </>
  );
};
