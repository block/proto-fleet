import TabsComponent from ".";

export const Tabs = () => {
  return (
    <TabsComponent>
      <TabsComponent.Tab label="Tab one">Content one</TabsComponent.Tab>
      <TabsComponent.Tab label="Tab two">Content two</TabsComponent.Tab>
      <TabsComponent.Tab label="Tab three">Content three</TabsComponent.Tab>
    </TabsComponent>
  );
};

export default {
  title: "Shared/Tabs",
};
