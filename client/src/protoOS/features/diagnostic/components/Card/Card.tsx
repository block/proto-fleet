import { ReactNode } from "react";

type CardProps = {
  children: ReactNode;
};

function Card({ children }: CardProps) {
  return (
    <div className="@container flex flex-col gap-6 rounded-xl bg-core-primary-5 p-4" data-testid="card">
      {children}
    </div>
  );
}

export default Card;
