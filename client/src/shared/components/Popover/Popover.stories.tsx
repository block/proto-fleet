import { ElementType, useState } from "react";

import PopoverComponent, { PopoverProvider, usePopover } from ".";
import { variants } from "@/shared/components/Button";
import { ButtonProps } from "@/shared/components/ButtonGroup";
import { useClickOutside } from "@/shared/hooks/useClickOutside";

interface PopoverProps {
  hasSubtitle: boolean;
  numberOfButtons: number;
}

export const Popover = ({ hasSubtitle, numberOfButtons }: PopoverProps) => {
  const [showPopover, setShowPopover] = useState(true);
  const { triggerRef } = usePopover();

  useClickOutside({
    ref: triggerRef,
    onClickOutside: () => setShowPopover(false),
  });

  return (
    <div ref={triggerRef}>
      <button onClick={() => setShowPopover((prev) => !prev)}>Show Popover</button>
      {showPopover ? (
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
                  variant: variants.primary,
                }),
              },
            ].filter((button) => !!button.text) as ButtonProps[]
          }
          position="bottom right"
        />
      ) : null}
    </div>
  );
};

export default {
  title: "Shared/Popover",
  decorators: [
    (Story: ElementType) => (
      <PopoverProvider>
        <Story />
      </PopoverProvider>
    ),
  ],
  component: Popover,
  parameters: {
    docs: {
      description: {
        component:
          "Popover component to display a popover with optional title, subtitle, and buttons. " +
          "The popover is positioned relative to a trigger element and will adjust its position to avoid overflow. " +
          "To supply a trigger element, use the `usePopover` hook together with `PopoverProvider`.\n\n" +
          "When supplying trigger element, you should also specify whether the trigger element has fixed position on the page. " +
          "The default value is false (element is not fixed). " +
          "Popover with fixed trigger element is rendered as child element of the trigger element. " +
          "Otherwise, popover is rendered as child element of the body. " +
          "This way we avoid usage of scroll listeners in both cases.",
      },
    },
  },
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
  tags: ["autodocs"],
};
