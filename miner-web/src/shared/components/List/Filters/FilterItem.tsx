import Button, { sizes } from "@/shared/components/Button";
import StatusCircle from "@/shared/components/StatusCircle";
import { StatusCircleStatus } from "@/shared/components/StatusCircle/constants";

type FilterItemProps<FilterType> = {
  status?: StatusCircleStatus;
  count?: number;
  filter: FilterType;
  title: string;
  activeFilter: FilterType;
  setActiveFilter: (filter: FilterType) => void;
};

const FilterItem = <FilterType,>({
  status,
  count,
  filter,
  title,
  activeFilter,
  setActiveFilter,
}: FilterItemProps<FilterType>) => {
  return (
    <Button
      size={sizes.compact}
      variant={activeFilter === filter ? "primary" : "ghost"}
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

export default FilterItem;
