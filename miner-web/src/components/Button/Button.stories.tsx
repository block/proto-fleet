import { action } from "@storybook/addon-actions";

import { BaseIcon, CompactIcon } from "common/stories/icons";

import Button, { sizes, variants } from ".";

const onClick = action("Button clicked");

interface ButtonProps {
  size: keyof typeof sizes;
  variant: keyof typeof variants;
}

export const Buttons = ({ size, variant }: ButtonProps) => {
  const Icon = size === sizes.base ? BaseIcon : CompactIcon;
  const text = "Button";

  return (
    <div className="flex flex-col space-y-4">
      <div className="flex space-x-2">
        <Button text={text} onClick={onClick} size={size} variant={variant} />
        <Button
          text={text}
          disabled
          onClick={onClick}
          size={size}
          variant={variant}
        />
      </div>
      <div className="flex space-x-2">
        <Button
          prefixIcon={<Icon />}
          onClick={onClick}
          size={size}
          variant={variant}
        />
        <Button
          prefixIcon={<Icon />}
          disabled
          onClick={onClick}
          size={size}
          variant={variant}
        />
      </div>
      <div className="flex space-x-2">
        <Button
          text={text}
          prefixIcon={<Icon />}
          onClick={onClick}
          size={size}
          variant={variant}
        />
        <Button
          text={text}
          prefixIcon={<Icon />}
          disabled
          onClick={onClick}
          size={size}
          variant={variant}
        />
      </div>
      <div className="flex space-x-2">
        <Button
          text={text}
          suffixIcon={<Icon />}
          onClick={onClick}
          size={size}
          variant={variant}
        />
        <Button
          text={text}
          suffixIcon={<Icon />}
          disabled
          onClick={onClick}
          size={size}
          variant={variant}
        />
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
    options: [sizes.base, sizes.compact],
  },
  variant: {
    control: "select",
    options: [
      variants.primary,
      variants.accent,
      variants.secondary,
      variants.danger,
      variants.secondaryDanger,
    ],
  },
};

// since size isn't changeable in textOnly variant, have it in a separate story
export const TextOnly = () => {
  return (
    <div className="flex space-x-2">
      <Button
        text="Text only button"
        onClick={onClick}
        size={sizes.textOnly}
        variant={variants.textOnly}
      />
      <Button
        text="Text only button"
        disabled
        onClick={onClick}
        size={sizes.textOnly}
        variant={variants.textOnly}
      />
    </div>
  );
};

export default {
  title: "Buttons",
};
