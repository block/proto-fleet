import { action } from "@storybook/addon-actions";

import HeaderComponent from ".";
import { variants } from "@/shared/components/Button";
import { ButtonProps } from "@/shared/components/ButtonGroup";
import { BaseIcon } from "@/shared/stories/icons";

const buttons = {
  both: "both",
  primary: "primary",
  none: "none",
} as const;

interface HeaderProps {
  button: keyof typeof buttons;
  hasIcon: boolean;
  hasTitle: boolean;
  inline: boolean;
  titleSize: string;
}

export const Header = ({
  button,
  hasIcon,
  hasTitle,
  inline,
  titleSize,
}: HeaderProps) => {
  const iconProps = hasIcon
    ? { icon: <BaseIcon />, iconOnClick: action("Icon clicked") }
    : {};
  const hasButton = button === buttons.primary || button === buttons.both;
  const buttonProps = {
    buttons: hasButton
      ? ([
          {
            ...(hasButton && {
              text: "Primary",
              onClick: action("Primary button clicked"),
              variant: variants.primary,
            }),
          },
          {
            ...(button === buttons.both && {
              text: "Secondary",
              onClick: action("Secondary button clicked"),
              variant: variants.secondary,
            }),
          },
        ].filter((button) => !!button.text) as ButtonProps[])
      : undefined,
  };

  const titleProps = hasTitle ? { title: "Title" } : {};

  return (
    <HeaderComponent
      {...iconProps}
      {...buttonProps}
      {...titleProps}
      titleSize={titleSize}
      inline={inline}
    />
  );
};

const fontSizes = ["text-heading-100", "text-heading-200"];

export default {
  title: "Components (Shared)/Header",
  component: Header,
  args: {
    button: buttons.both,
    hasIcon: true,
    hasTitle: true,
    inline: true,
    titleSize: "text-heading-200",
  },
  argTypes: {
    button: { control: "select", options: Object.keys(buttons) },
    hasIcon: { control: "boolean" },
    hasTitle: { control: "boolean" },
    inline: { control: "boolean" },
    titleSize: { control: "select", options: fontSizes },
  },
};
