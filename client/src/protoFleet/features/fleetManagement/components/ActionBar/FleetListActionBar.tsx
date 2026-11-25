import type { ComponentProps, ReactNode } from "react";
import clsx from "clsx";
import { createPortal } from "react-dom";
import ActionBar from "@/shared/components/ActionBar";

type FleetListActionBarProps = Omit<ComponentProps<typeof ActionBar>, "className" | "renderActions"> & {
  isActionBusy: boolean;
  children: ReactNode;
};

const FleetListActionBar = ({ isActionBusy, children, ...props }: FleetListActionBarProps) =>
  // Escape the app shell's stacking context so menus sit above the header,
  // while remaining below the phone/tablet navigation drawer.
  createPortal(
    <ActionBar
      {...props}
      className={clsx("fixed right-0 bottom-4 left-0 z-[45] tablet:left-16 desktop:left-50", {
        invisible: isActionBusy,
      })}
      renderActions={() => children}
    />,
    document.body,
  );

export default FleetListActionBar;
