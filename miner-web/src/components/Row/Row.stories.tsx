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
    onClick: clickable ? () => action("Row clicked")() : undefined,
  };

  return (
    <div className="w-96 px-4">
      <RowComponent {...rowProps}>Row</RowComponent>
      <RowComponent {...rowProps} divider={!removeLastDivider}>Row</RowComponent>
    </div>
  );
};
Row.args = {
  clickable: false,
  compact: false,
  removeLastDivider: true,
};
Row.argTypes = {
  compact: {
    clickable: {
      type: "boolean",
    },
    control: {
      type: "boolean",
    },
    removeLastDivdier: {
      type: "boolean",
    },
  },
};

export default {
  title: "Row",
};
