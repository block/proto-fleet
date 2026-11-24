import ContainerInventoryPage from "./ContainerInventoryPage";

export const SixTanks = () => (
  <ContainerInventoryPage
    title="Tanks"
    emptyMessage="No tanks found."
    items={Array.from({ length: 6 }, (_, index) => ({
      id: `tank-${index + 1}`,
      label: `Tank ${index + 1}`,
      details: ["48 modules", "144 hashboards"],
    }))}
  />
);

export default {
  title: "Proto OS/Containers/Container inventory page",
  component: ContainerInventoryPage,
};
