import { useState } from "react";
import { action } from "storybook/actions";

import ModalComponent from ".";
import Button, { sizes, variants } from "@/shared/components/Button";

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
      <div className="mt-16 flex w-full justify-center">
        <div className="flex flex-col">
          <div className="mb-2 text-400">Content behind the overlay</div>
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
  title: "Components (Shared)/Modal",
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
