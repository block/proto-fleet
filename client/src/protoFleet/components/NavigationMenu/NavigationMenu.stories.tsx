import { ElementType, useEffect, useState } from "react";
import { MemoryRouter } from "react-router-dom";

import { action } from "storybook/actions";
import NavigationMenuComponent from ".";
import { primaryNavItems } from "@/protoFleet/config/navItems";
import { useFleetStore } from "@/protoFleet/store";
import { Menu } from "@/shared/assets/icons";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

// Representative owner permissions for the navigation; role is only a display label.
const navigationPermissions = [
  "fleet:read",
  "miner:read",
  "rack:read",
  "site:read",
  "maintenance:read",
  "curtailment:read",
  "activity:read",
  "pool:manage",
  "miner:firmware_update",
  "fleetnode:read",
  "schedule:manage",
  "curtailment:manage",
  "alert:read",
  "user:read",
  "role:manage",
  "apikey:manage",
  "serverlog:read",
  "instance:update",
];

export const NavigationMenu = ({
  username,
  role,
  permissions,
}: {
  username: string;
  role: string;
  permissions: string[];
}) => {
  useEffect(() => {
    const {
      username: previousUsername,
      role: previousRole,
      permissions: previousPermissions,
    } = useFleetStore.getState().auth;
    useFleetStore.setState((state) => ({ auth: { ...state.auth, username, role, permissions } }));
    return () => {
      useFleetStore.setState((state) => ({
        auth: { ...state.auth, username: previousUsername, role: previousRole, permissions: previousPermissions },
      }));
    };
  }, [username, role, permissions]);

  const [isOpen, setIsOpen] = useState(false);
  const { isDesktop } = useWindowDimensions();
  if (isDesktop && isOpen) setIsOpen(false);

  return (
    <>
      <NavigationMenuComponent
        items={primaryNavItems}
        isVisible={isOpen}
        closeMenu={() => {
          setIsOpen(false);
          action("close menu")();
        }}
      />
      <div className="p-4 tablet:ml-16 desktop:ml-50">
        {!isDesktop ? (
          <Menu ariaLabel="Open navigation menu" ariaExpanded={isOpen} onClick={() => setIsOpen(true)} />
        ) : null}
      </div>
    </>
  );
};

export default {
  title: "Proto Fleet/NavigationMenu",
  parameters: {
    withRouter: false,
  },
  args: { username: "achen", role: "SUPER_ADMIN", permissions: navigationPermissions },
  argTypes: {
    username: { control: "text" },
    role: { control: "text" },
    permissions: { control: "object", description: "Permission keys used to filter navigation entries." },
  },
  decorators: [
    (Story: ElementType) => (
      <MemoryRouter initialEntries={["/settings/network"]}>
        <Story />
      </MemoryRouter>
    ),
  ],
};
