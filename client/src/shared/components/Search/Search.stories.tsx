import { action } from "storybook/actions";

import SearchComponent, { SearchVariant } from ".";

interface SearchProps {
  variant: SearchVariant;
}

export const Search = ({ variant }: SearchProps) => {
  return <SearchComponent onChange={(value) => action("onChange search")(value)} variant={variant} />;
};

/** The three variants side by side, since the difference between them is
 * entirely visual: `compact` has no container of its own and disappears into
 * the page unless the caller supplies one. */
export const AllVariants = () => {
  const variants: SearchVariant[] = ["default", "toolbar", "compact"];
  return (
    <div className="flex flex-col gap-8 p-4">
      {variants.map((variant) => (
        <div key={variant} className="flex flex-col gap-2">
          <span className="text-200 text-text-primary-50">{variant}</span>
          <SearchComponent
            id={`search-${variant}`}
            onChange={(value) => action(`onChange ${variant}`)(value)}
            variant={variant}
          />
        </div>
      ))}
    </div>
  );
};

export default {
  title: "Shared/Search",
  args: {
    variant: "toolbar",
  },
  argTypes: {
    variant: {
      control: "select",
      options: ["default", "toolbar", "compact"],
    },
  },
};
