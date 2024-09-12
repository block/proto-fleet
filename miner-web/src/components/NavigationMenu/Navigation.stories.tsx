import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";

import Navigation from ".";

interface NavigationSidebarProps {
  hasMacAddress: boolean;
  hasVersion: boolean;
  MacAddressLoading: boolean;
  MacAddressValue: string;
  versionLoading: boolean;
  versionValue: string;
}

export const NavigationSidebar = ({
  hasMacAddress,
  hasVersion,
  MacAddressLoading,
  MacAddressValue,
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
    />
  );
};

export default {
  title: "Components/Navigation Sidebar",
  args: {
    hasMacAddress: true,
    hasVersion: true,
    MacAddressLoading: false,
    MacAddressValue: "42.08.59.58.84.c6",
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
