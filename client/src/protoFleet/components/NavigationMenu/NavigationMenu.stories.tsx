import { ElementType, useEffect, useState } from "react";
import { MemoryRouter } from "react-router-dom";

import { action } from "storybook/actions";
import NavigationMenuComponent from ".";
import { primaryNavItems } from "@/protoFleet/config/navItems";
import { useFleetStore } from "@/protoFleet/store";
import { Menu } from "@/shared/assets/icons";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

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
