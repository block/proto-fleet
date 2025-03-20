import Chip from ".";
import { CompactIcon } from "@/shared/stories/icons";

interface ChipArgs {
  loading: boolean;
}

export const Chips = ({ loading }: ChipArgs) => {
  const text = "Alerts";

  return (
    <div className="flex flex-col space-y-4">
      <Chip prefixIcon={<CompactIcon />} loading={loading}>
        {text}
      </Chip>
      <Chip loading={loading}>{text}</Chip>
    </div>
  );
};

Chips.args = {
  loading: false,
};

export default {
  title: "Components (Shared)/Chips",
};
