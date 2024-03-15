import Tab, { TabWrapper } from ".";

export const Tabs = () => {
  return (
    <TabWrapper>
      <Tab label={"Tab one"}>Content one</Tab>
      <Tab label={"Tab two"}>Content two</Tab>
      <Tab label={"Tab three"}>Content three</Tab>
    </TabWrapper>
  );
};

export default {
  title: "Components/Tabs",
};
