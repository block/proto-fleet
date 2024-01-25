import clsx from "clsx";

import { badgeColorClassName } from "./constants";

export type BadgeStatus = keyof typeof badgeColorClassName;

interface BadgeProps {
  status?: BadgeStatus;
}

const Badge = ({ status }: BadgeProps) => {
  return (
    <span
      className={clsx(
        "rounded-[3px] w-[18px] h-2",
        status && badgeColorClassName[status]
      )}
    />
  );
};

export default Badge;
