import { action } from "@storybook/addon-actions";

import { sizes, variants } from "components/Button";

import ButtonGroup, { groupVariants } from ".";

const primaryButton = {
  onClick: action("Primary button clicked"),
  text: "Primary",
  variant: variants.primary,
};

const secondaryButton = {
  onClick: action("Secondary button clicked"),
  text: "Secondary",
  variant: variants.secondary,
};

interface Props {
  size: keyof typeof sizes;
  variant: keyof typeof groupVariants;
}

export const Groups = ({ size, variant }: Props) => {
  return (
    <div className="flex flex-col space-y-4 w-[600px]">
      <ButtonGroup buttons={[primaryButton]} variant={variant} size={size} />
      <ButtonGroup
        buttons={[primaryButton, secondaryButton]}
        variant={variant}
        size={size}
      />
      <ButtonGroup
        buttons={[primaryButton, secondaryButton, secondaryButton]}
        variant={variant}
        size={size}
      />
      <ButtonGroup
        buttons={[
          primaryButton,
          secondaryButton,
          secondaryButton,
          secondaryButton,
        ]}
        variant={variant}
        size={size}
      />
    </div>
  );
};

Groups.args = {
  size: sizes.base,
  variant: groupVariants.rightAligned,
};

// eslint-disable-next-line @typescript-eslint/no-unused-vars
const { textOnly, ...rest } = groupVariants;
Groups.argTypes = {
  size: {
    control: "select",
    options: [sizes.base, sizes.compact],
  },
  variant: {
    control: "select",
    options: Object.keys(rest),
  },
};

// since size isn't changeable in textOnly variant, have it in a separate story
export const TextOnly = () => {
  const size = sizes.textOnly;

  return <Groups size={size} variant={groupVariants.textOnly} />;
};

export default {
  title: "Components/Button Groups",
};
