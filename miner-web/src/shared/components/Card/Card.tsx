import { ReactNode } from "react";
import clsx from "clsx";

import { cardType } from ".";

interface CardProps {
  children: ReactNode;
  title: string;
  type: (typeof cardType)[keyof typeof cardType];
}

const Card = ({ children, title, type }: CardProps) => {
  return (
    <div className="rounded-xl shadow-50">
      <div
        className={clsx("rounded-t-xl px-4 py-2", {
          "text-text-primary bg-core-primary-5": type === cardType.default,
          "text-text-contrast bg-intent-success-fill":
            type === cardType.success,
          "text-text-contrast bg-intent-critical-fill":
            type === cardType.warning,
        })}
      >
        {title}
      </div>
      <div className="px-4">{children}</div>
    </div>
  );
};

export default Card;
