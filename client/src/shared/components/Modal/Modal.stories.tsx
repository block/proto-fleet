import { useState } from "react";
import { action } from "storybook/actions";

import ModalComponent, { sizes as modalSizes } from ".";
import Button, { sizes, variants } from "@/shared/components/Button";

interface ModalProps {
  hasButtons: boolean;
  hasTitle: boolean;
  numberOfSecondaryButtons: number;
}

export const Modal = ({ hasButtons, hasTitle, numberOfSecondaryButtons }: ModalProps) => {
  const secondaryButton = {
    text: "Secondary",
    onClick: action("Secondary button clicked"),
    variant: variants.secondary,
  };

  const [showModal, setShowModal] = useState(true);

  return (
    <>
      <div className="mt-16 flex w-full justify-center">
        <div className="flex flex-col">
          <div className="mb-2 text-400">Content behind the overlay</div>
          <Button onClick={() => setShowModal(true)} text="Show Modal" variant={variants.primary} size={sizes.base} />
        </div>
      </div>
      {showModal && (
        <ModalComponent
          title={hasTitle ? "Title" : undefined}
          contentHeader="Content"
          buttons={
            hasButtons
              ? [
                  {
                    text: "Primary",
                    onClick: action("Primary button clicked"),
                    variant: variants.primary,
                  },
                  ...Array(numberOfSecondaryButtons).fill(secondaryButton),
                ]
              : undefined
          }
          onDismiss={() => setShowModal(false)}
        >
          <div>Description</div>
        </ModalComponent>
      )}
    </>
  );
};

export default {
  title: "Shared/Modal",
  component: Modal,
  args: {
    hasButtons: true,
    hasTitle: true,
    numberOfSecondaryButtons: 1,
  },
  argTypes: {
    hasButtons: { control: "boolean" },
    hasTitle: { control: "boolean" },
    numberOfSecondaryButtons: { control: "select", options: [0, 1, 2] },
  },
};

// Fullscreen variant
export const Fullscreen = () => {
  const [showModal, setShowModal] = useState(true);

  return (
    <>
      <div className="mt-16 flex w-full justify-center">
        <div className="flex flex-col">
          <div className="mb-2 text-400">Content behind the overlay</div>
          <Button
            onClick={() => setShowModal(true)}
            text="Show Fullscreen Modal"
            variant={variants.primary}
            size={sizes.base}
          />
        </div>
      </div>
      {showModal && (
        <ModalComponent
          title="Fullscreen Modal"
          contentHeader="This modal takes up the full screen"
          size={modalSizes.fullscreen}
          buttons={[
            {
              text: "Close",
              onClick: action("Close button clicked"),
              variant: variants.primary,
            },
          ]}
          onDismiss={() => setShowModal(false)}
        >
          <div className="p-4">
            <p>This is a fullscreen modal that takes up the entire viewport.</p>
            <p className="mt-2">It's useful for immersive experiences or when you need maximum space for content.</p>
          </div>
        </ModalComponent>
      )}
    </>
  );
};

// No header variant
export const NoHeader = () => {
  const [showModal, setShowModal] = useState(true);

  return (
    <>
      <div className="mt-16 flex w-full justify-center">
        <div className="flex flex-col">
          <div className="mb-2 text-400">Content behind the overlay</div>
          <Button
            onClick={() => setShowModal(true)}
            text="Show Modal Without Header"
            variant={variants.primary}
            size={sizes.base}
          />
        </div>
      </div>
      {showModal && (
        <ModalComponent
          showHeader={false}
          buttons={[
            {
              text: "Got it",
              onClick: action("Confirm button clicked"),
              variant: variants.primary,
            },
            {
              text: "Cancel",
              onClick: () => setShowModal(false),
              variant: variants.secondary,
            },
          ]}
          onDismiss={() => setShowModal(false)}
        >
          <div className="py-4">
            <h2 className="mb-2 text-heading-200">Custom Content Area</h2>
            <p>This modal has no header section (no title, description, or close icon).</p>
            <p className="mt-2">
              This is useful for custom layouts where you want full control over the modal content.
            </p>
            <p className="mt-2">The modal can still be closed with Escape key or clicking outside.</p>
          </div>
        </ModalComponent>
      )}
    </>
  );
};
