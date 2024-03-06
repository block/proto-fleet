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
        "rounded-[200px] w-3 h-1",
        status && badgeColorClassName[status]
      )}
    />
  );
};

export default Badge;
