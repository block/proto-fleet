import { useState } from "react";
import { action } from "storybook/actions";
import AuthenticateFleetModal from "./AuthenticateFleetModal";

export default {
  title: "Proto Fleet/Auth/AuthenticateFleetModal",
  component: AuthenticateFleetModal,
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
      <AuthenticateFleetModal
        open={open}
        onAuthenticated={(username, password) => {
          action("onAuthenticated")({ username, password });
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

export const SecurityPurpose = () => {
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
      <AuthenticateFleetModal
        open={open}
        purpose="security"
        onAuthenticated={(username, password) => {
          action("onAuthenticated")({ username, password });
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

export const PoolPurpose = () => {
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
      <AuthenticateFleetModal
        open={open}
        purpose="pool"
        onAuthenticated={(username, password) => {
          action("onAuthenticated")({ username, password });
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
