import React from "react";
import clsx from "clsx";

interface ComponentSectionProps {
  title: React.ReactNode;
  children: React.ReactNode;
  className?: string;
}

function ComponentSection({ title, children, className }: ComponentSectionProps) {
  return (
    <section className={clsx("flex flex-col gap-3", className)} data-testid={`component-section-${title}`}>
      <h2 className="text-heading-200">{title}</h2>
      {children}
    </section>
  );
}

export default ComponentSection;
