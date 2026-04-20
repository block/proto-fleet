import { action } from "storybook/actions";

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
  hasEyebrow: boolean;
  hasTitle: boolean;
  hasSubtitle: boolean;
  hasDescription: boolean;
  inline: boolean;
  titleSize: string;
}

export const Header = ({
  button,
  hasIcon,
  hasEyebrow,
  hasTitle,
  hasSubtitle,
  hasDescription,
  inline,
  titleSize,
}: HeaderProps) => {
  const iconProps = hasIcon
    ? {
        icon: <BaseIcon />,
        iconAriaLabel: "Story icon action",
        iconOnClick: action("Icon clicked"),
      }
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

  const eyebrowProps = hasEyebrow ? { eybrow: "Eyebrow" } : {};
  const titleProps = hasTitle ? { title: "Title" } : {};
  const subtitleProps = hasSubtitle ? { subtitle: "Subtitle" } : {};
  const descriptionProps = hasDescription ? { description: "Description" } : {};

  return (
    <HeaderComponent
      {...iconProps}
      {...buttonProps}
      {...eyebrowProps}
      {...titleProps}
      {...subtitleProps}
      {...descriptionProps}
      titleSize={titleSize}
      inline={inline}
    />
  );
};

const fontSizes = ["text-heading-100", "text-heading-200", "text-heading-300"];

export default {
  title: "Shared/Header",
  component: Header,
  args: {
    button: buttons.both,
    hasIcon: true,
    hasEyebrow: true,
    hasTitle: true,
    hasSubtitle: true,
    hasDescription: true,
    inline: true,
    titleSize: "text-heading-200",
  },
  argTypes: {
    button: { control: "select", options: Object.keys(buttons) },
    hasIcon: { control: "boolean" },
    hasEyebrow: { control: "boolean" },
    hasTitle: { control: "boolean" },
    hasSubtitle: { control: "boolean" },
    hasDescription: { control: "boolean" },
    inline: { control: "boolean" },
    titleSize: { control: "select", options: fontSizes },
  },
};
