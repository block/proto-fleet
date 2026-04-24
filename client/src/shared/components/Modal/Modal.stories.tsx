import { useState } from "react";
import { action } from "storybook/actions";

import ModalComponent from ".";
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
      {showModal ? (
        <ModalComponent
          title={hasTitle ? "Title" : undefined}
          description="This is a description that stays in the content area."
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
      ) : null}
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

// Standard variant (640px) — default size
export const Standard = () => {
  const [showModal, setShowModal] = useState(true);

  return (
    <>
      <div className="mt-16 flex w-full justify-center">
        <div className="flex flex-col">
          <div className="mb-2 text-400">Content behind the overlay</div>
          <Button
            onClick={() => setShowModal(true)}
            text="Show Standard Modal"
            variant={variants.primary}
            size={sizes.base}
          />
        </div>
      </div>
      {showModal ? (
        <ModalComponent
          title="Standard Modal"
          description="Form Content"
          size="standard" // explicit for demo purposes
          buttons={[
            {
              text: "Save",
              onClick: action("Save button clicked"),
              variant: variants.primary,
            },
          ]}
          onDismiss={() => setShowModal(false)}
        >
          <div className="mt-4 flex flex-col gap-4">
            <p>This modal is 640px max-width, the default size for forms and general content.</p>
          </div>
        </ModalComponent>
      ) : null}
    </>
  );
};

// Large variant (dynamic, max 1280px)
export const Large = () => {
  const [showModal, setShowModal] = useState(true);

  return (
    <>
      <div className="mt-16 flex w-full justify-center">
        <div className="flex flex-col">
          <div className="mb-2 text-400">Content behind the overlay</div>
          <Button
            onClick={() => setShowModal(true)}
            text="Show Large Modal"
            variant={variants.primary}
            size={sizes.base}
          />
        </div>
      </div>
      {showModal ? (
        <ModalComponent
          title="Large Modal"
          description="Data-Heavy Content"
          size="large"
          buttons={[
            {
              text: "Done",
              onClick: action("Done button clicked"),
              variant: variants.primary,
            },
          ]}
          onDismiss={() => setShowModal(false)}
        >
          <div className="mt-4 flex flex-col gap-4">
            <p>
              This modal flexes to the viewport width (minus margins) with a max of 1280px, suitable for tables and
              data-heavy content.
            </p>
          </div>
        </ModalComponent>
      ) : null}
    </>
  );
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
      {showModal ? (
        <ModalComponent
          title="Fullscreen Modal"
          description="This modal takes up the full screen"
          size="fullscreen"
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
      ) : null}
    </>
  );
};

// Long content — demonstrates scroll-aware title collapse
export const LongContent = () => {
  const [showModal, setShowModal] = useState(true);

  return (
    <>
      <div className="mt-16 flex w-full justify-center">
        <div className="flex flex-col">
          <div className="mb-2 text-400">Content behind the overlay</div>
          <Button
            onClick={() => setShowModal(true)}
            text="Show Long Content Modal"
            variant={variants.primary}
            size={sizes.base}
          />
        </div>
      </div>
      {showModal ? (
        <ModalComponent
          title="Scroll-Aware Title"
          description="Scroll down — the title collapses into the sticky header when it leaves the viewport."
          buttons={[
            {
              text: "Done",
              onClick: action("Done button clicked"),
              variant: variants.primary,
            },
          ]}
          onDismiss={() => setShowModal(false)}
        >
          <div className="mt-4 flex flex-col gap-4">
            {Array.from({ length: 20 }, (_, i) => (
              <p key={i}>
                Paragraph {i + 1} — Lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor
                incididunt ut labore et dolore magna aliqua. Ut enim ad minim veniam, quis nostrud exercitation ullamco
                laboris nisi ut aliquip ex ea commodo consequat.
              </p>
            ))}
          </div>
        </ModalComponent>
      ) : null}
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
      {showModal ? (
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
      ) : null}
    </>
  );
};
