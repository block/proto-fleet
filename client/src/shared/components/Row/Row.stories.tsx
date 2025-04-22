import { action } from "@storybook/addon-actions";

import RowComponent from ".";

interface RowProps {
  clickable: boolean;
  compact: boolean;
  removeLastDivider: boolean;
}

export const Row = ({ clickable, compact, removeLastDivider }: RowProps) => {
  const rowProps = {
    compact,
    onClick: clickable ? action("Row clicked") : undefined,
  };

  return (
    <div className="w-96 px-4">
      <RowComponent {...rowProps}>Row</RowComponent>
      <RowComponent {...rowProps} divider={!removeLastDivider}>
        Row
      </RowComponent>
    </div>
  );
};

export default {
  title: "Components (Shared)/Row",
  args: {
    clickable: false,
    compact: false,
    removeLastDivider: true,
  },
  argTypes: {
    compact: {
      clickable: "boolean",
      control: "boolean",
      removeLastDivider: "boolean",
    },
  },
};
