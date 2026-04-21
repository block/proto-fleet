import { useState } from "react";
import { action } from "storybook/actions";
import DeactivateUserDialog from "./DeactivateUserDialog";

export default {
  title: "Proto Fleet/Settings/DeactivateUserDialog",
  component: DeactivateUserDialog,
};

// Default story
export const Default = () => {
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
    <DeactivateUserDialog
      username="john_doe"
      onConfirm={() => {
        action("onConfirm")();
        setShow(false);
      }}
      onDismiss={() => {
        action("onDismiss")();
        setShow(false);
      }}
      isSubmitting={false}
    />
  );
};

// Loading state
export const LoadingState = () => {
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
    <DeactivateUserDialog
      username="john_doe"
      onConfirm={() => {
        action("onConfirm")();
      }}
      onDismiss={() => {
        action("onDismiss")();
        setShow(false);
      }}
      isSubmitting={true}
    />
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
    <DeactivateUserDialog
      username="john_doe_with_a_very_long_username_for_testing"
      onConfirm={() => {
        action("onConfirm")();
        setShow(false);
      }}
      onDismiss={() => {
        action("onDismiss")();
        setShow(false);
      }}
      isSubmitting={false}
    />
  );
};

// Interactive demo
export const Interactive = () => {
  const [show, setShow] = useState(true);
  const [isSubmitting, setIsSubmitting] = useState(false);

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
    <div>
      <div className="mb-4 rounded-lg bg-intent-warning-10 p-4 text-300 text-text-primary">
        Click "Confirm deactivation" to simulate a 2-second deactivation process
      </div>
      <DeactivateUserDialog
        username="jane_smith"
        onConfirm={() => {
          action("onConfirm")();
          setIsSubmitting(true);
          setTimeout(() => {
            setIsSubmitting(false);
            setShow(false);
          }, 2000);
        }}
        onDismiss={() => {
          action("onDismiss")();
          setShow(false);
        }}
        isSubmitting={isSubmitting}
      />
    </div>
  );
};
