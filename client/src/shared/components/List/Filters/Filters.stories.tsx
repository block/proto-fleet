import FiltersComponent from "./index";
import { testFilters, testItems } from "@/shared/components/List/mocks/data";

interface FiltersArgs {
  numberOfFilters: number;
}

export const Filters = ({ numberOfFilters }: FiltersArgs) => {
  return (
    <FiltersComponent
      className="gap-4 py-4"
      filterItems={testFilters.slice(0, numberOfFilters)}
      items={testItems}
      onFilter={() => {}}
    />
  );
};

export default {
  title: "Shared/List/Filters",
  parameters: {
    docs: {
      description: {
        component:
          "A reusable and configurable filter component for refining displayed data in a list. \n" +
          "It supports:\n " +
          " - Customizable filter items with title, count, and status.\n " +
          " - Dynamic filtering logic through the `onFilter` callback.\n " +
          " - Integration with list components for seamless data filtering.",
      },
    },
  },
  args: {
    numberOfFilters: 5,
  },
  argTypes: {
    numberOfFilters: { control: { type: "range", min: 1, max: 5, step: 1 } },
  },
  tags: ["autodocs"],
};
