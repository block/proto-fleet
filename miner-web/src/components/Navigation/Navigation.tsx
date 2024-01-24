import { useCallback, useEffect, useMemo, useState } from "react";
import { useLocation } from "react-router-dom";
import clsx from "clsx";

import { Api } from "Api";

import dashboardIcon from "assets/dashboard.svg";
import hardwareIcon from "assets/hardware.svg";
import helpIcon from "assets/help.svg";
import setupIcon from "assets/setup.svg";

import { useLocalStorage } from "common/hooks/useLocalStorage";

import { navigationItems, showIdentifiersLocalStorageKey } from "./constants";
import InfoItem from "./InfoItem";
import NavigationButton from "./NavigationButton";
import NavigationItem from "./NavigationItem";

import "./styles.css";

interface NavigationProps {
  controller_ip?: string;
  controller_mac?: string;
  hashboard_serials?: (string | undefined)[];
}

const { api } = new Api();

const Navigation = ({
  controller_ip,
  controller_mac,
  hashboard_serials = [],
}: NavigationProps) => {
  const location = useLocation();
  const { pathname } = location;
  const pageName = pathname.split("/")[1] as keyof typeof navigationItems;

  const { setItem, getItem } = useLocalStorage();

  const [selected, setSelected] = useState(
    (navigationItems[pageName] ||
      navigationItems.dashboard) as keyof typeof navigationItems
  );
  const [selectedHashboard, setSelectedHashboard] = useState<
    string | undefined
  >();
  const [hashboardDropdownOpen, setHashboardDropdownOpen] = useState(false);
  const [showIdentifiers, setShowIdentifiers] = useState(
    getItem(showIdentifiersLocalStorageKey) ?? true
  );

  const toggleHashboardDropdown = useCallback(() => {
    setHashboardDropdownOpen(!hashboardDropdownOpen);
  }, [hashboardDropdownOpen]);

  const toggleIdentifiers = useCallback(() => {
    setShowIdentifiers(!showIdentifiers);
    setItem(showIdentifiersLocalStorageKey, !showIdentifiers);
  }, [setItem, showIdentifiers]);

  const selectHashboard = useCallback((serial: string) => {
    setSelectedHashboard(serial);
    setHashboardDropdownOpen(false);
  }, []);

  useEffect(() => {
    if (!selectedHashboard && hashboard_serials.length) {
      setSelectedHashboard(hashboard_serials[0]);
    }
  }, [hashboard_serials, selectedHashboard]);

  const selectedHashboardLabel = useMemo(() => {
    if (selectedHashboard) {
      return (
        hashboard_serials.findIndex(
          (hashboard_serial) => hashboard_serial === selectedHashboard
        ) + 1
      );
    }
    return "";
  }, [hashboard_serials, selectedHashboard]);

  const shouldShowHashboardDropdown = useMemo(() => {
    return hashboard_serials.length > 1;
  }, [hashboard_serials]);

  return (
    <div className="sidebar-wrapper w-[280px] h-screen p-6 flex flex-col">
      <div className="grow">
        <div className="text-title-1 mb-10">BTC Miner</div>
        <NavigationItem
          icon={dashboardIcon}
          id={navigationItems.dashboard}
          text="Dashboard"
          selected={selected}
          setSelected={setSelected}
        />
        <NavigationItem
          icon={hardwareIcon}
          id={navigationItems.hardware}
          text="Hardware"
          selected={selected}
          setSelected={setSelected}
        />
        <NavigationItem
          icon={setupIcon}
          id={navigationItems.setup}
          text="Setup"
          selected={selected}
          setSelected={setSelected}
        />
        <NavigationItem
          icon={helpIcon}
          id={navigationItems.help}
          text="Help"
          selected={selected}
          setSelected={setSelected}
        />
      </div>

      {showIdentifiers && (
        <>
          <div className="border-t-[1px] border-foreground-100/10 mt-11 mb-3" />

          <div className="relative">
            <InfoItem
              caret={shouldShowHashboardDropdown}
              handleClick={
                shouldShowHashboardDropdown
                  ? toggleHashboardDropdown
                  : undefined
              }
              label={`Hashboard #${selectedHashboardLabel} Serial`}
              value={selectedHashboard}
            />

            {hashboardDropdownOpen && (
              <div className="w-[232px] bg-white-100 p-4 rounded-md shadow-lg absolute z-10 top-6 -left-1">
                {hashboard_serials.map((serial, index) => (
                  <div
                    className={clsx(
                      "hover:cursor-pointer rounded-lg p-2 h-[35px] flex items-center border-b-[1px] border-foreground-100/5",
                      {
                        "bg-primary-100/10": serial === selectedHashboard,
                      }
                    )}
                    key={serial}
                    onClick={() => serial && selectHashboard(serial)}
                  >
                    Hashboard #{index + 1}
                  </div>
                ))}
              </div>
            )}
          </div>

          <InfoItem label="Controller Board IP Address" value={controller_ip} />
          <InfoItem label="Controller MAC Address" value={controller_mac} />
        </>
      )}

      <div className="border-t-[1px] border-foreground-100/10 mb-3" />

      <div
        className="text-primary-100 text-body-default mb-4 hover:cursor-pointer select-none"
        onClick={toggleIdentifiers}
      >
        {showIdentifiers ? "Hide" : "Show"} Identifiers
      </div>

      <NavigationButton text="Sleep" className="mb-3" onClick={api.stopMining} />
      <NavigationButton text="Reboot" className="mb-3" onClick={api.rebootSystem} />
      <NavigationButton text="Update firmware" onClick={() => {}} />
    </div>
  );
};

export default Navigation;
