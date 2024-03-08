import { useState } from "react";
import { action } from "@storybook/addon-actions";

import Button, { sizes, variants } from "components/Button";

import ModalComponent from ".";

interface ModalProps {
  hasButtons: boolean;
  hasTitle: boolean;
  numberOfSecondaryButtons: number;
}

export const Modal = ({
  hasButtons,
  hasTitle,
  numberOfSecondaryButtons,
}: ModalProps) => {
  const secondaryButton = {
    text: "Secondary",
    onClick: action("Secondary button clicked"),
    variant: variants.secondary,
  };

  const [showModal, setShowModal] = useState(true);

  return (
    <>
    <div className="flex w-full justify-center mt-16">
      <div className="flex flex-col">
        <div className="text-400 mb-2">Content behind the overlay</div>
        <Button
          onClick={() => setShowModal(true)}
          text="Show Modal"
          variant={variants.primary}
          size={sizes.base}
        />
      </div>
    </div>
      {showModal && (
        <ModalComponent
          title={hasTitle ? "Title" : undefined}
          contentHeader="Content"
          buttons={hasButtons ? [
            {
              text: "Primary",
              onClick: action("Primary button clicked"),
              variant: variants.primary,
            },
            ...Array(numberOfSecondaryButtons).fill(secondaryButton),
          ] : undefined}
          onDismiss={() => setShowModal(false)}
        >
          <div>Description</div>
        </ModalComponent>
      )}
    </>
  );
};

export default {
  title: "Modal",
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
