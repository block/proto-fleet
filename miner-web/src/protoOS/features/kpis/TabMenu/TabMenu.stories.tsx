import TabMenuComponent from "./TabMenuWrapper";

export const TabMenu = () => {
  return <TabMenuComponent />;
};

export default {
  title: "protoOS/Components/TabMenu",
  decorators: [
    (Story: any) => (
      <div className="min-h-[100vh] w-full flex justify-center items-center">
        <Story />
      </div>
    ),
  ],
};
