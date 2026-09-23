import { ElementType, useEffect } from "react";
import { MemoryRouter } from "react-router-dom";

import { action } from "storybook/actions";
import NavigationMenuComponent from ".";
import { primaryNavItems } from "@/protoFleet/config/navItems";
import { useFleetStore } from "@/protoFleet/store";

export const NavigationMenu = ({ username, role }: { username: string; role: string }) => {
  useEffect(() => {
    const { username: previousUsername, role: previousRole } = useFleetStore.getState().auth;
    useFleetStore.setState((state) => ({ auth: { ...state.auth, username, role } }));
    return () => {
      useFleetStore.setState((state) => ({
        auth: { ...state.auth, username: previousUsername, role: previousRole },
      }));
    };
  }, [username, role]);

  return <NavigationMenuComponent items={primaryNavItems} isVisible={true} closeMenu={action("close menu")} />;
};

export default {
  title: "Proto Fleet/NavigationMenu",
  parameters: {
    withRouter: false,
  },
  args: { username: "achen", role: "SUPER_ADMIN" },
  argTypes: { username: { control: "text" }, role: { control: "text" } },
  decorators: [
    (Story: ElementType) => (
      <MemoryRouter initialEntries={["/settings/network"]}>
        <Story />
      </MemoryRouter>
    ),
  ],
};
