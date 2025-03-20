import { Globe } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Chip from "@/shared/components/Chip";
import SkeletonBar from "@/shared/components/SkeletonBar";

interface LocationSelectorProps {
  location?: string;
  loading?: boolean;
}

const LocationSelector = ({ location, loading }: LocationSelectorProps) => {
  // TODO implement selector with options
  return (
    <Chip prefixIcon={<Globe width={iconSizes.small} />}>
      {loading ? <SkeletonBar className="w-20" /> : <>{location}</>}
    </Chip>
  );
};

export default LocationSelector;
