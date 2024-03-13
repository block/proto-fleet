import { action } from "@storybook/addon-actions";

import { variants } from "components/Button";
import { ButtonProps } from "components/ButtonGroup";

import PopoverComponent from ".";

interface PopoverProps {
  hasSubtitle: boolean;
  numberOfButtons: number;
}

export const Popover = ({ hasSubtitle, numberOfButtons }: PopoverProps) => {
  return (
    <PopoverComponent
      title="Title"
      subtitle={hasSubtitle ? "Subtitle" : undefined}
      buttons={
        [
          {
            ...(numberOfButtons >= 1 && {
              text: "Cancel",
              onClick: action("Cancel clicked"),
              variant: variants.secondary,
            }),
          },
          {
            ...(numberOfButtons === 2 && {
              text: "Apply",
              onClick: action("Apply clicked"),
              variant: variants.accent,
            }),
          },
        ].filter((button) => !!button.text) as ButtonProps[]
      }
    />
  );
};

export default {
  title: "Popover",
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
