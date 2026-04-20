import Button, { sizes } from "@/shared/components/Button";
import StatusCircle from "@/shared/components/StatusCircle";
import { StatusCircleStatus } from "@/shared/components/StatusCircle/constants";

type ButtonFilterProps = {
  status?: StatusCircleStatus;
  count?: number;
  filter: string;
  title: string;
  activeFilters: string[];
  setActiveFilter: (filter: string) => void;
  size?: keyof typeof sizes;
};

const ButtonFilter = ({
  status,
  count,
  filter,
  title,
  activeFilters,
  setActiveFilter,
  size = sizes.compact,
}: ButtonFilterProps) => {
  const isActive = activeFilters.includes(filter);

  return (
    <Button
      size={size}
      variant={isActive ? "accent" : "ghost"}
      onClick={() => setActiveFilter(filter)}
      prefixIcon={status && <StatusCircle status={status} width="w-2" variant="simple" removeMargin={true} />}
      testId={`filter-button-${filter}`}
    >
      {title} {count !== undefined && <span className="opacity-50">{count}</span>}
    </Button>
  );
};

export default ButtonFilter;
