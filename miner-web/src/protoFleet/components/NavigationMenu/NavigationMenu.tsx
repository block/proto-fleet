import { createElement } from "react";
import { Link } from "react-router-dom";
import clsx from "clsx";

import { navigationItems } from "./constants";
import { LogoAlt } from "@/shared/assets/icons";

const NavigationMenu = () => {
  return (
    <div
      className={clsx(
        "w-[64px] min-h-screen flex flex-col bg-core-grayscale-gray-5 bg-surface-5 text-text-primary-70 border-r border-border-5",
        "tablet:min-h-[calc(100vh-16px)] tablet:z-30 tablet:absolute tablet:rounded-lg",
        "phone:min-h-[calc(100vh-16px)] phone:z-30 phone:absolute phone:rounded-lg"
      )}
    >
      <div className="flex items-center justify-center flex-col gap-[10px]">
        <div className="h-[60px] px-3 py-2 flex items-center justify-center">
          <Link to={`/${navigationItems.home}`}>
            <LogoAlt className="hover:cursor-pointer text-text-primary" />
          </Link>
        </div>
        {Object.values(navigationItems).map((item, idx) => {
          return (
            <div
              key={idx}
              className="w-[40px] h-[40px] flex items-center justify-center"
            >
              <Link to={`/${item.route}`}>
                {createElement(item.icon, {
                  className:
                    "hover:cursor-pointer text-text-primary border-red",
                })}
              </Link>
            </div>
          );
        })}
      </div>
    </div>
  );
};

export default NavigationMenu;
