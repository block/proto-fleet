import PoolsListComponent from ".";
import { SelectType, selectTypes } from "@/shared/constants";

interface PoolsListArgs {
  title: string;
  subtitle: string;
  selectType: SelectType;
  createNewLabel: string;
}

export const PoolsList = ({
  title,
  subtitle,
  selectType,
  createNewLabel,
}: PoolsListArgs) => {
  const availablePools = [
    {
      poolUrl: "stratum+tcp://mine.ocean.xyz:3334",
      username: "mann23",
    },
    {
      poolUrl: "stratum+tcp://mine.ocean.xyz:3323",
      username: "mann25",
    },
  ];

  return (
    <PoolsListComponent
      title={title}
      subtitle={subtitle}
      availablePools={availablePools}
      selectType={selectType}
      selectedPools={["stratum+tcp://mine.ocean.xyz:3334"]}
      onSelect={() => {}}
      createNewLabel={createNewLabel}
    />
  );
};

export default {
  title: "Proto Fleet/Action Bar/Settings widget/Pools modal/Pools list",
  args: {
    title: "Default pool",
    subtitle: "Select one default pool",
    selectType: selectTypes.radio,
    createNewLabel: "Add default pool",
  },
  argTypes: {
    title: {
      control: "text",
    },
    subtitle: {
      control: "text",
    },
    selectType: {
      control: "select",
      options: Object.values(selectTypes),
    },
    createNewLabel: {
      control: "text",
    },
  },
};
