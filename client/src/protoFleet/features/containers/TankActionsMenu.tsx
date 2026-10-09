import type { ComponentProps } from "react";
import MiningActionsMenu from "./MiningActionsMenu";
export default function TankActionsMenu({
  tanks,
  ...props
}: Omit<ComponentProps<typeof MiningActionsMenu>, "kind" | "items"> & {
  tanks: ComponentProps<typeof MiningActionsMenu>["items"];
}) {
  return <MiningActionsMenu {...props} items={tanks} kind="tank" />;
}
