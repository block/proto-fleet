import { ReactNode } from "react";

type CardProps = {
  children: ReactNode;
};

function Card({ children }: CardProps) {
  return (
    <div className="@container flex flex-col gap-6 rounded-xl bg-surface-5 p-4">
      {children}
    </div>
  );
}

export default Card;
