import { createElement } from "react";
import ContainerHardware from "./ContainerHardware";
import RigHardware from "./Hardware";
import { useDeviceType } from "@/protoOS/store";

const Hardware = () => createElement(useDeviceType() === "container" ? ContainerHardware : RigHardware);

export default Hardware;
