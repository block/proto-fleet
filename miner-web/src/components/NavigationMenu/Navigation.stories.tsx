import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";

import Navigation from ".";

interface NavigationSidebarProps {
  hasMacAddress: boolean;
  MacAddressLoading: boolean;
  MacAddressValue: string;
}

export const NavigationSidebar = ({
  hasMacAddress,
  MacAddressLoading,
  MacAddressValue,
}: NavigationSidebarProps) => {
  return (
    <Navigation
      macInfo={{
        loading: MacAddressLoading,
        value: hasMacAddress ? MacAddressValue : undefined,
      }}
    />
  );
};

export default {
  title: "Components/Navigation Sidebar",
  args: {
    hasMacAddress: true,
    MacAddressLoading: false,
    MacAddressValue: "42.08.59.58.84.c6",
  },
  argTypes: {
    hasMacAddress: {
      control: "boolean",
    },
    MacAddressLoading: {
      control: "boolean",
    },
    MacAddressValue: {
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
