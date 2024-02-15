import PauseIcon from "assets/icons/Pause";
import PowerIcon from "assets/icons/Power";

import ControllerIpAddressInfo from "./InfoItems/ControllerIpAddressInfo";
import ControllerMacAddressInfo from "./InfoItems/ControllerMacAddressInfo";
import HashboardInfo from "./InfoItems/HashboardInfo";
import PoolInfo, { PoolProps } from "./InfoItems/PoolInfo";
import NavigationButton from "./NavigationButton";
import NavigationItems from "./NavigationItems";

import "./styles.css";

interface ApiProps {
  loading?: boolean;
}

interface InfoItemProps extends ApiProps {
  value?: string;
}

interface HashboardProps extends ApiProps {
  value?: string[];
}

interface NavigationProps {
  controller_ip?: InfoItemProps;
  controller_mac?: InfoItemProps;
  hashboard_serials?: HashboardProps;
  pool_info?: PoolProps;
  onClickReboot: () => void;
  onClickSleep: () => void;
}

const Navigation = ({
  controller_ip,
  controller_mac,
  hashboard_serials,
  pool_info,
  onClickReboot,
  onClickSleep,
}: NavigationProps) => {
  return (
    <div className="sidebar-wrapper w-[280px] h-auto min-h-screen p-6 flex flex-col border-r border-border-primary/30">
      <div className="grow">
        <div className="text-heading-300 mb-6 text-text-primary/70">
          Proto<span className="text-text-primary">Mine</span>
        </div>
        <NavigationItems />
      </div>

      <div className="border-t border-border-primary/10 mt-11 mb-3" />

      <PoolInfo
        status={pool_info?.status}
        url={pool_info?.url}
        loading={pool_info?.loading}
        error={pool_info?.error}
      />
      <HashboardInfo
        hashboard_serials={hashboard_serials?.value}
        loading={hashboard_serials?.loading}
      />
      <ControllerIpAddressInfo
        ip_address={controller_ip?.value}
        loading={controller_ip?.loading}
      />
      <ControllerMacAddressInfo
        mac_address={controller_mac?.value}
        loading={controller_mac?.loading}
      />

      <div className="border-t border-border-primary/10 mb-3" />

      <div className="flex space-x-3">
        <NavigationButton
          text="Sleep"
          className="w-full"
          prefixIcon={<PauseIcon />}
          onClick={onClickSleep}
        />
        <NavigationButton
          text="Reboot"
          className="w-full"
          prefixIcon={<PowerIcon />}
          onClick={onClickReboot}
        />
      </div>
    </div>
  );
};

export default Navigation;
