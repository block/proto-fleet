import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";

import type { StoryFn } from "@storybook/react";
import { action } from "storybook/actions";
import NavigationMenuComponent from ".";
import { primaryNavItems } from "@/protoFleet/config/navItems";
import { useFleetStore } from "@/protoFleet/store";

export const NavigationMenu: StoryFn = () => {
  return <NavigationMenuComponent items={primaryNavItems} isVisible={true} closeMenu={action("close menu")} />;
};

// Nav entries are permission-gated; seed the full set so the story shows the
// complete menu. site:read additionally makes scopable links site-scoped, as
// they are for a typical operator.
const navPermissions = primaryNavItems.flatMap((item) => [
  ...(item.requiredPermission ? [item.requiredPermission] : []),
  ...(item.requiredAnyPermission ?? []).flatMap((requirement) =>
    Array.isArray(requirement) ? requirement : [requirement],
  ),
]);

const seedNavPermissions = () => {
  const previousPermissions = useFleetStore.getState().auth.permissions;
  useFleetStore.setState((state) => ({
    auth: { ...state.auth, permissions: [...new Set([...previousPermissions, ...navPermissions, "site:read"])] },
  }));
  return () => useFleetStore.setState((state) => ({ auth: { ...state.auth, permissions: [...previousPermissions] } }));
};
NavigationMenu.beforeEach = seedNavPermissions;

export default {
  title: "Proto Fleet/NavigationMenu",
  // Item visibility is permission-driven, so pin every story to an empty
  // permission baseline instead of whatever permissions an interrupted
  // story left persisted in the iframe's localStorage; seeded stories layer
  // on top of this in their own beforeEach (story hooks run after meta's).
  beforeEach: () => {
    const previousPermissions = useFleetStore.getState().auth.permissions;
    useFleetStore.setState((state) => ({
      auth: { ...state.auth, permissions: [] },
    }));
    return () => {
      useFleetStore.setState((state) => ({ auth: { ...state.auth, permissions: [...previousPermissions] } }));
    };
  },
  parameters: {
    withRouter: false,
  },
  args: {},
  argTypes: {},
  decorators: [
    (Story: ElementType) => (
      <MemoryRouter initialEntries={["/settings/network"]}>
        <Story />
      </MemoryRouter>
    ),
  ],
};
