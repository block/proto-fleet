import LocationSelector from "./LocationSelector";

const LocationSelectorWrapper = () => {
  // TODO load location from API
  const location = "Proto Fleet Beta";
  const loading = false;

  return <LocationSelector location={location} loading={loading} />;
};

export default LocationSelectorWrapper;
