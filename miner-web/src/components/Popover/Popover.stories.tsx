import { useRef, useState } from "react";

import { useClickOutside } from "common/hooks/useClickOutside";

import { variants } from "components/Button";
import { ButtonProps } from "components/ButtonGroup";

import PopoverComponent from ".";

interface PopoverProps {
  hasSubtitle: boolean;
  numberOfButtons: number;
}

export const Popover = ({ hasSubtitle, numberOfButtons }: PopoverProps) => {
  const [showPopover, setShowPopover] = useState(true);
  const ref = useRef<HTMLDivElement>(null);

  useClickOutside({ ref, onClickOutside: () => setShowPopover(false) });

  return (
    <div ref={ref}>
      <button onClick={() => setShowPopover((prev) => !prev)}>
        Show Popover
      </button>
      {showPopover && (
        <PopoverComponent
          title="Title"
          subtitle={hasSubtitle ? "Subtitle" : undefined}
          buttons={
            [
              {
                ...(numberOfButtons >= 1 && {
                  text: "Cancel",
                  onClick: () => setShowPopover(false),
                  variant: variants.secondary,
                }),
              },
              {
                ...(numberOfButtons === 2 && {
                  text: "Apply",
                  onClick: () => setShowPopover(false),
                  variant: variants.accent,
                }),
              },
            ].filter((button) => !!button.text) as ButtonProps[]
          }
          position="bottom right"
        />
      )}
    </div>
  );
};

export default {
  title: "Components/Popover",
  args: {
    hasSubtitle: true,
    numberOfButtons: 2,
  },
  argTypes: {
    hasSubtitle: {
      control: "boolean",
    },
    numberOfButtons: {
      control: "select",
      options: [0, 1, 2],
    },
  },
};
