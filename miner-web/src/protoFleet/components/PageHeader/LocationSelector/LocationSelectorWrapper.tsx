import LocationSelector from "./LocationSelector";

const LocationSelectorWrapper = () => {
  // TODO load location from API
  const location = "ProtoFleet test lab";
  const loading = false;

  return <LocationSelector location={location} loading={loading} />;
};

export default LocationSelectorWrapper;
