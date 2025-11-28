import { useState } from "react";
import { action } from "storybook/actions";
import ResetPasswordModal from "./ResetPasswordModal";

export default {
  title: "Proto Fleet/Settings/ResetPasswordModal",
  component: ResetPasswordModal,
};

// Step 1: Confirmation prompt
export const ConfirmationStep = () => {
  const [show, setShow] = useState(true);

  if (!show) {
    return (
      <div className="flex h-screen items-center justify-center">
        <button onClick={() => setShow(true)} className="bg-emphasis-300 rounded-lg px-4 py-2 text-surface-base">
          Show Modal
        </button>
      </div>
    );
  }

  return (
    <ResetPasswordModal
      username="john_doe"
      temporaryPassword={null}
      onConfirm={() => {
        action("onConfirm")();
      }}
      onDismiss={() => {
        action("onDismiss")();
        setShow(false);
      }}
      isResetting={false}
    />
  );
};

// Step 1: Confirmation with loading state
export const ConfirmationLoading = () => {
  const [show, setShow] = useState(true);

  if (!show) {
    return (
      <div className="flex h-screen items-center justify-center">
        <button onClick={() => setShow(true)} className="bg-emphasis-300 rounded-lg px-4 py-2 text-surface-base">
          Show Modal
        </button>
      </div>
    );
  }

  return (
    <ResetPasswordModal
      username="john_doe"
      temporaryPassword={null}
      onConfirm={() => {
        action("onConfirm")();
      }}
      onDismiss={() => {
        action("onDismiss")();
        setShow(false);
      }}
      isResetting={true}
    />
  );
};

// Step 2: Success with temporary password
export const SuccessStep = () => {
  const [show, setShow] = useState(true);

  if (!show) {
    return (
      <div className="flex h-screen items-center justify-center">
        <button onClick={() => setShow(true)} className="bg-emphasis-300 rounded-lg px-4 py-2 text-surface-base">
          Show Modal
        </button>
      </div>
    );
  }

  return (
    <ResetPasswordModal
      username="john_doe"
      temporaryPassword="TempPass123!@#"
      onConfirm={() => {
        action("onConfirm")();
      }}
      onDismiss={() => {
        action("onDismiss")();
        setShow(false);
      }}
      isResetting={false}
    />
  );
};

// Interactive full flow
export const InteractiveFullFlow = () => {
  const [show, setShow] = useState(true);
  const [temporaryPassword, setTemporaryPassword] = useState<string | null>(null);
  const [isResetting, setIsResetting] = useState(false);

  if (!show) {
    return (
      <div className="flex h-screen items-center justify-center">
        <button
          onClick={() => {
            setShow(true);
            setTemporaryPassword(null);
            setIsResetting(false);
          }}
          className="bg-emphasis-300 rounded-lg px-4 py-2 text-surface-base"
        >
          Show Modal
        </button>
      </div>
    );
  }

  return (
    <div>
      <div className="mb-4 rounded-lg bg-intent-warning-10 p-4 text-300 text-text-primary">
        Click "Reset member password" to simulate the reset flow (2 second delay)
      </div>
      <ResetPasswordModal
        username="jane_smith"
        temporaryPassword={temporaryPassword}
        onConfirm={() => {
          action("onConfirm")();
          if (!temporaryPassword) {
            setIsResetting(true);
            setTimeout(() => {
              setTemporaryPassword("TempPass456$%^");
              setIsResetting(false);
            }, 2000);
          }
        }}
        onDismiss={() => {
          action("onDismiss")();
          setShow(false);
        }}
        isResetting={isResetting}
      />
    </div>
  );
};

// Long username
export const LongUsername = () => {
  const [show, setShow] = useState(true);

  if (!show) {
    return (
      <div className="flex h-screen items-center justify-center">
        <button onClick={() => setShow(true)} className="bg-emphasis-300 rounded-lg px-4 py-2 text-surface-base">
          Show Modal
        </button>
      </div>
    );
  }

  return (
    <ResetPasswordModal
      username="john_doe_with_a_very_long_username_for_testing"
      temporaryPassword="TempPass789&*()"
      onConfirm={() => {
        action("onConfirm")();
      }}
      onDismiss={() => {
        action("onDismiss")();
        setShow(false);
      }}
      isResetting={false}
    />
  );
};
