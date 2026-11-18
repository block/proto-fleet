import type { ReactNode } from "react";

/**
 * The update artifacts for one device type on the firmware Files tab. Each
 * child is spaced as one block, so wrap a component that renders several root
 * elements (List does) in a single element.
 */
export const FirmwareDeviceSection = ({ title, children }: { title: string; children: ReactNode }) => (
  <section className="flex flex-col gap-4">
    <h2 className="text-heading-200 text-text-primary">{title}</h2>
    <div className="flex flex-col gap-6">{children}</div>
  </section>
);

/** One kind of artifact within a device type, with its own upload action. */
export const FirmwareArtifactSection = ({
  title,
  action,
  children,
}: {
  title: string;
  action?: ReactNode;
  children: ReactNode;
}) => (
  <section className="flex flex-col gap-4">
    <div className="flex items-center justify-between gap-4 phone:flex-col phone:items-stretch">
      <h3 className="text-emphasis-300 text-text-primary">{title}</h3>
      {action}
    </div>
    <div>{children}</div>
  </section>
);
