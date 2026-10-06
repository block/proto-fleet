import { ElementType, useEffect, useState } from "react";
import { MemoryRouter } from "react-router-dom";
import { create } from "@bufbuild/protobuf";

import { action } from "storybook/actions";
import NavigationRailToggle from "./NavigationRailToggle";
import { useNavigationRail } from "./useNavigationRail";
import NavigationMenuComponent from ".";
import { SiteSchema, SiteWithCountsSchema } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import SitePicker from "@/protoFleet/components/PageHeader/SitePicker";
import { primaryNavItems } from "@/protoFleet/config/navItems";
import { useFleetStore } from "@/protoFleet/store";
import { Menu } from "@/shared/assets/icons";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

const sites = [
  create(SiteWithCountsSchema, {
    site: create(SiteSchema, { id: 1n, name: "Block LA", slug: "block-la" }),
  }),
];

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
  const rail = useNavigationRail();
  const { isPhone } = useWindowDimensions();
  if (!isPhone && isOpen) setIsOpen(false);

  return (
    <>
      <NavigationMenuComponent
        items={primaryNavItems}
        rail={rail}
        isVisible={isOpen}
        closeMenu={() => {
          setIsOpen(false);
          action("close menu")();
        }}
      />
      <div
        className={`relative z-20 flex h-12 items-center px-4 laptop:h-15 ${rail.isExpanded ? "tablet:ml-50" : "tablet:ml-16"} desktop:ml-50`}
      >
        <NavigationRailToggle rail={rail} />
        {isPhone ? (
          <Menu
            ariaLabel="Open navigation menu"
            ariaExpanded={isOpen}
            className="mr-2 text-text-primary"
            onClick={() => setIsOpen(true)}
          />
        ) : null}
        <SitePicker sites={sites} />
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
