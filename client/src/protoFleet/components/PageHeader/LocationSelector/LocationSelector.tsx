import SkeletonBar from "@/shared/components/SkeletonBar";

interface LocationSelectorProps {
  location?: string;
  loading?: boolean;
}

const LocationSelector = ({ location, loading }: LocationSelectorProps) => {
  // TODO implement selector with options
  return <div className="text-300 text-text-primary">{loading ? <SkeletonBar className="w-20" /> : location}</div>;
};

export default LocationSelector;
