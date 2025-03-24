import { MemoryRouter } from "react-router-dom";
import TabMenuComponent from "./TabMenuWrapper";

export const TabMenu = () => {
  return (
    <TabMenuComponent
      hashrate={102.51}
      efficiency={34.42}
      powerUsage={2.03}
      temperature={63.21}
    />
  );
};

export default {
  title: "protoOS/TabMenu",
  decorators: [
    (Story: any) => (
      <MemoryRouter initialEntries={["/hashrate"]}>
        <div className="flex min-h-[100vh] w-full items-center justify-center">
          <Story />
        </div>
      </MemoryRouter>
    ),
  ],
};
