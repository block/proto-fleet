import { action } from "storybook/actions";

import ButtonGroup, { groupVariants } from ".";
import { sizes, variants } from "@/shared/components/Button";

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
    <div className="flex w-[600px] flex-col space-y-4">
      <ButtonGroup buttons={[primaryButton]} variant={variant} size={size} />
      <ButtonGroup buttons={[primaryButton, secondaryButton]} variant={variant} size={size} />
      <ButtonGroup buttons={[primaryButton, secondaryButton, secondaryButton]} variant={variant} size={size} />
      <ButtonGroup
        buttons={[primaryButton, secondaryButton, secondaryButton, secondaryButton]}
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
  title: "Shared/Button Groups",
};
