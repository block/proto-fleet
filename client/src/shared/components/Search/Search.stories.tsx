import { action } from "storybook/actions";

import SearchComponent from ".";

interface SearchProps {
  compact: boolean;
}

export const Search = ({ compact }: SearchProps) => {
  return <SearchComponent onChange={(value) => action("onChange search")(value)} compact={compact} />;
};

export default {
  title: "Shared/Search",
  args: {
    compact: false,
  },
  argTypes: {
    compact: {
      control: "boolean",
    },
  },
};
