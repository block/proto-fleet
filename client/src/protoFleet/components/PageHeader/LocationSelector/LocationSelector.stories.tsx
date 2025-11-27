import LocationSelectorComponent from "./LocationSelector";

interface LocationSelectorArgs {
  loading: boolean;
  location: string;
}

export const LocationSelector = ({ loading, location }: LocationSelectorArgs) => {
  return <LocationSelectorComponent loading={loading} location={location} />;
};

export default {
  title: "Proto Fleet/Page Header/Location Selector",
  args: {
    loading: false,
    location: "ProtoFleet test lab",
  },
};
