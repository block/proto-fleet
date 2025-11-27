import { action } from "storybook/actions";
import Chip from ".";
import { CompactIcon } from "@/shared/stories/icons";

interface ChipArgs {
  loading: boolean;
  clickable: boolean;
}

export const Chips = ({ loading, clickable }: ChipArgs) => {
  const text = "Alerts";

  return (
    <div className="flex flex-col space-y-4">
      <Chip prefixIcon={<CompactIcon />} loading={loading} onClick={clickable ? action("clicked") : undefined}>
        {text}
      </Chip>
      <Chip loading={loading} onClick={clickable ? action("clicked") : undefined}>
        {text}
      </Chip>
    </div>
  );
};

Chips.args = {
  loading: false,
  clickable: false,
};

export default {
  title: "Shared/Chips",
};
