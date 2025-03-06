import FiltersComponent from ".";

export const Filters = () => {
  return (
    <FiltersComponent miners={[]} setFilteredMiners={(miners) => void miners} />
  );
};

export default {
  title: "Components (ProtoFleet)/MinerList/Filters",
};
