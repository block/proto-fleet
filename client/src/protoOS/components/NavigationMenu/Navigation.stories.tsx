import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";

import Navigation, { NavigationMenuType, navigationMenuTypes } from ".";

interface NavigationSidebarProps {
  hasMacAddress: boolean;
  hasVersion: boolean;
  MacAddressLoading: boolean;
  MacAddressValue: string;
  type: NavigationMenuType;
  versionLoading: boolean;
  versionValue: string;
}

export const NavigationSidebar = ({
  hasMacAddress,
  hasVersion,
  MacAddressLoading,
  MacAddressValue,
  type,
  versionLoading,
  versionValue,
}: NavigationSidebarProps) => {
  return (
    <Navigation
      macInfo={{
        loading: MacAddressLoading,
        value: hasMacAddress ? MacAddressValue : undefined,
      }}
      versionInfo={{
        loading: versionLoading,
        value: hasVersion ? versionValue : undefined,
      }}
      isVisible
      type={type}
    />
  );
};

export default {
  title: "Proto OS/Navigation Sidebar",
  parameters: {
    withRouter: false,
  },
  args: {
    hasMacAddress: true,
    hasVersion: true,
    MacAddressLoading: false,
    MacAddressValue: "42.08.59.58.84.c6",
    type: navigationMenuTypes.app,
    versionLoading: false,
    versionValue: "1.2.3",
  },
  argTypes: {
    hasMacAddress: {
      control: "boolean",
    },
    hasVersion: {
      control: "boolean",
    },
    MacAddressLoading: {
      control: "boolean",
    },
    MacAddressValue: {
      control: "text",
    },
    type: {
      control: "select",
      options: Object.values(navigationMenuTypes),
    },
    versionLoading: {
      control: "boolean",
    },
    versionValue: {
      control: "text",
    },
  },
  decorators: [
    (Story: ElementType) => (
      <MemoryRouter>
        <Story />
      </MemoryRouter>
    ),
  ],
};
