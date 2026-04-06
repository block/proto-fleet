import { action } from "storybook/actions";

import Button, { type ButtonVariant, sizes, variants } from ".";
import { BaseIcon, CompactIcon } from "@/shared/stories/icons";

const onClick = action("Button clicked");

interface ButtonProps {
  size: keyof typeof sizes;
  variant: ButtonVariant;
}

export const Buttons = ({ size, variant }: ButtonProps) => {
  const Icon = size === sizes.base ? BaseIcon : CompactIcon;
  const text = "Button";

  return (
    <div className="flex flex-col space-y-4">
      <div className="flex space-x-2">
        <Button text={text} onClick={onClick} size={size} variant={variant} />
        <Button text={text} disabled onClick={onClick} size={size} variant={variant} />
      </div>
      <div className="flex space-x-2">
        <Button ariaLabel="Icon button" prefixIcon={<Icon />} onClick={onClick} size={size} variant={variant} />
        <Button
          ariaLabel="Disabled icon button"
          prefixIcon={<Icon />}
          disabled
          onClick={onClick}
          size={size}
          variant={variant}
        />
      </div>
      <div className="flex space-x-2">
        <Button text={text} prefixIcon={<Icon />} onClick={onClick} size={size} variant={variant} />
        <Button text={text} prefixIcon={<Icon />} disabled onClick={onClick} size={size} variant={variant} />
      </div>
      <div className="flex space-x-2">
        <Button text={text} suffixIcon={<Icon />} onClick={onClick} size={size} variant={variant} />
        <Button text={text} suffixIcon={<Icon />} disabled onClick={onClick} size={size} variant={variant} />
      </div>
    </div>
  );
};

Buttons.args = {
  size: sizes.base,
  variant: variants.primary,
};
Buttons.argTypes = {
  size: {
    control: "select",
    options: Object.keys(sizes).filter((key) => key !== "textOnly"),
  },
  variant: {
    control: "select",
    options: Object.keys(variants).filter((key) => key !== "textOnly"),
  },
};

// since size isn't changeable in textOnly variant, have it in a separate story
export const TextOnly = () => {
  return (
    <div className="flex space-x-2">
      <Button text="Text only button" onClick={onClick} size={sizes.textOnly} variant={variants.textOnly} />
      <Button text="Text only button" disabled onClick={onClick} size={sizes.textOnly} variant={variants.textOnly} />
    </div>
  );
};

export default {
  title: "Shared/Buttons",
};
