import { useState } from "react";
import { action } from "storybook/actions";

import InfoModalComponent from "./InfoModal";
import Button, { sizes, variants } from "@/shared/components/Button";

interface InfoModalProps {
  hasButtons: boolean;
}

const InfoModalStory = ({ hasButtons }: InfoModalProps) => {
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
        <InfoModalComponent
          onDismiss={() => setShowModal(false)}
          buttons={
            hasButtons
              ? [
                  {
                    text: "Enter sleep mode",
                    onClick: action("Primary button clicked"),
                    variant: variants.primary,
                  },
                ]
              : undefined
          }
        />
      )}
    </>
  );
};

export const WithButton = () => <InfoModalStory hasButtons={true} />;

export const WithoutButton = () => <InfoModalStory hasButtons={false} />;

export default {
  title: "ProtoOS/Settings/Cooling/InfoModal",
};
