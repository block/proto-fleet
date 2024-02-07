import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useLocation } from "react-router-dom";
import clsx from "clsx";

import { Api, Pool } from "Api";

import PauseIcon from "assets/icons/Pause";
import PowerIcon from "assets/icons/Power";

import { useClickOutside } from "common/hooks/useClickOutside";

import { navigationItems } from "./constants";
import InfoItem from "./InfoItem";
import NavigationButton from "./NavigationButton";
import NavigationItem from "./NavigationItem";

import "./styles.css";

interface NavigationProps {
  controller_ip?: string;
  controller_mac?: string;
  hashboard_serials?: (string | undefined)[];
  pool_info?: { status?: Pool["status"]; url?: Pool["url"] };
}

const { api } = new Api();

const Navigation = ({
  controller_ip,
  controller_mac,
  hashboard_serials = [],
  pool_info = { status: undefined, url: undefined },
}: NavigationProps) => {
  const location = useLocation();
  const { pathname } = location;
  const pageName = pathname.split("/")[1] as keyof typeof navigationItems;

  const isPoolConnected = useMemo(() => pool_info.status === "Alive", [pool_info]);
  const isPoolLoading = useMemo(() => !pool_info.status, [pool_info]);

  const [selected, setSelected] = useState(
    (navigationItems[pageName] ||
      navigationItems.performance) as keyof typeof navigationItems
  );
  const [selectedHashboard, setSelectedHashboard] = useState<
    string | undefined
  >();
  const [hashboardDropdownOpen, setHashboardDropdownOpen] = useState(false);
  const hashboardDropdownRef = useRef<HTMLDivElement>(null);

  const toggleHashboardDropdown = useCallback(() => {
    setHashboardDropdownOpen(!hashboardDropdownOpen);
  }, [hashboardDropdownOpen]);

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

  const onClickOutside = useCallback(() => {
    setHashboardDropdownOpen(false);
  }, []);

  useClickOutside({ ref: hashboardDropdownRef, onClickOutside });

  return (
    <div className="sidebar-wrapper w-[280px] h-screen p-6 flex flex-col border-r border-foreground-30">
      <div className="grow">
        <div className="text-title-1 mb-6 text-foreground-60">
          Proto<span className="text-foreground-100">Mine</span>
        </div>
        <NavigationItem
          id={navigationItems.performance}
          text="Performance"
          selected={selected}
          setSelected={setSelected}
        />
        <NavigationItem
          id={navigationItems.hardware}
          text="Hardware"
          selected={selected}
          setSelected={setSelected}
        />
        <NavigationItem
          id={navigationItems.settings}
          text="Settings"
          selected={selected}
          setSelected={setSelected}
        />
        <NavigationItem
          id={navigationItems.help}
          text="Help"
          selected={selected}
          setSelected={setSelected}
        />
      </div>

      <div className="border-t border-foreground-100/10 mt-11 mb-3" />

      <InfoItem
        label={`Pool Connection ${isPoolLoading || isPoolConnected ? "" : "(failed)"}`}
        value={pool_info.url}
        badge={isPoolConnected ? "success" : "error"}
        error={!isPoolLoading && !isPoolConnected}
      />

      <div className="relative">
        <InfoItem
          caret={shouldShowHashboardDropdown}
          handleClick={
            shouldShowHashboardDropdown ? toggleHashboardDropdown : undefined
          }
          label={`Hashboard ${selectedHashboardLabel} Serial`}
          value={selectedHashboard}
        />

        {hashboardDropdownOpen && (
          <div
            ref={hashboardDropdownRef}
            className="w-[232px] bg-foreground-20 p-4 rounded-md shadow-lg absolute z-10 top-5 -left-1 text-body-regular"
          >
            {hashboard_serials.map((serial, index) => (
              <div
                className={clsx(
                  "hover:cursor-pointer rounded-md px-2 h-[33px] flex items-center",
                  {
                    "bg-warning-100/10": serial === selectedHashboard,
                    "hover:bg-warning-100/5": serial !== selectedHashboard,
                    // only add this bottom border if not selected item and not one before the selected item
                    "border-b-2 border-black-100/5":
                      serial !== selectedHashboard &&
                      selectedHashboard !== hashboard_serials[index + 1],
                  }
                )}
                key={serial}
                onClick={() => serial && selectHashboard(serial)}
              >
                Hashboard {index + 1}
              </div>
            ))}
          </div>
        )}
      </div>

      <InfoItem label="Controller Board IP Address" value={controller_ip} />
      <InfoItem label="Controller MAC Address" value={controller_mac} />

      <div className="border-t border-foreground-100/10 mb-3" />

      <div className="flex space-x-3">
        <NavigationButton
          text="Sleep"
          className="w-full"
          prefixIcon={<PauseIcon />}
          onClick={api.stopMining}
        />
        <NavigationButton
          text="Reboot"
          className="w-full"
          prefixIcon={<PowerIcon />}
          onClick={api.rebootSystem}
        />
      </div>
    </div>
  );
};

export default Navigation;
