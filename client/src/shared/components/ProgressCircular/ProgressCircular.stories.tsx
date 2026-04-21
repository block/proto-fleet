import { action } from "storybook/actions";
import ProgressCircularComponent from ".";
import Button, { sizes, variants } from "@/shared/components/Button";

interface ProgressCircularArgs {
  value: number;
}

export const ProgressCircular = ({ value }: ProgressCircularArgs) => {
  return (
    <div className="flex w-fit flex-col gap-4">
      <ProgressCircularComponent indeterminate />
      <ProgressCircularComponent value={value} />
      <Button
        onClick={action("Test Connection")}
        disabled
        size={sizes.compact}
        text="Test Connection"
        loading
        variant={variants.secondary}
      />
    </div>
  );
};

export default {
  title: "Shared/Progress/Progress Circular",
  args: {
    value: 0,
  },
  argTypes: {
    value: {
      control: { type: "range", min: 0, max: 100, step: 1 },
    },
  },
};
