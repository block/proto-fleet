import { type FilterType } from "./Filters";
import Button from "@/shared/components/Button";
import StatusCircle, {
  StatusCircleProps,
} from "@/shared/components/StatusCircle";

type FilterItemProps = {
  status?: StatusCircleProps["status"];
  count?: number;
  filter: FilterType;
  title: string;
  activeFilter: FilterType;
  setActiveFilter: (filter: FilterType) => void;
};

const FilterItem = ({
  status,
  count,
  filter,
  title,
  activeFilter,
  setActiveFilter,
}: FilterItemProps) => {
  return (
    <Button
      size="compact"
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
