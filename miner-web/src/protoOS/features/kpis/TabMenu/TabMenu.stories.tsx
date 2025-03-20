import TabMenuComponent from "./TabMenuWrapper";

export const TabMenu = () => {
  return <TabMenuComponent />;
};

export default {
  title: "protoOS/Components/TabMenu",
  decorators: [
    (Story: any) => (
      <div className="flex min-h-[100vh] w-full items-center justify-center">
        <Story />
      </div>
    ),
  ],
};
