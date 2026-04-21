import { MemoryRouter } from "react-router-dom";
import TabMenu from "./TabMenu";

export const CustomTabMenu = () => {
  const items = {
    profile: {
      name: "Profile",
      value: undefined,
      units: "",
      path: "/profile",
    },
    settings: {
      name: "Settings",
      value: undefined,
      units: "",
      path: "/settings",
    },
    notifications: {
      name: "Notifications",
      value: 3,
      units: "new",
      path: "/notifications",
    },
    help: {
      name: "Help",
      value: undefined,
      units: "",
      path: "/help",
    },
  };

  return <TabMenu items={items} />;
};

export default {
  title: "Shared/TabMenu",
  parameters: {
    withRouter: false,
  },
  decorators: [
    (Story: any) => (
      <MemoryRouter initialEntries={["/hashrate"]}>
        <div className="flex min-h-[100vh] w-full items-center justify-center px-16">
          <Story />
        </div>
      </MemoryRouter>
    ),
  ],
};
