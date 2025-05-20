import Button, { sizes } from "@/shared/components/Button";
import StatusCircle from "@/shared/components/StatusCircle";
import { StatusCircleStatus } from "@/shared/components/StatusCircle/constants";

type ButtonFilterProps<FilterType> = {
  status?: StatusCircleStatus;
  count?: number;
  filter: FilterType;
  title: string;
  activeFilters: FilterType[];
  setActiveFilter: (filter: FilterType) => void;
  size?: keyof typeof sizes;
};

const ButtonFilter = <FilterType,>({
  status,
  count,
  filter,
  title,
  activeFilters,
  setActiveFilter,
  size = sizes.compact,
}: ButtonFilterProps<FilterType>) => {
  const isActive = activeFilters.includes(filter);

  return (
    <Button
      size={size}
      variant={isActive ? "primary" : "ghost"}
      onClick={() => setActiveFilter(filter)}
      prefixIcon={
        status && (
          <StatusCircle
            status={status}
            width="w-2"
            variant="simple"
            removeMargin={true}
          />
        )
      }
    >
      {title}{" "}
      {count !== undefined && <span className="opacity-50">{count}</span>}
    </Button>
  );
};

export default ButtonFilter;
