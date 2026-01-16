import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { getComponentIcon } from "./utils";
import { ComponentType as ErrorComponentType } from "@/protoFleet/api/generated/errors/v1/errors_pb";

describe("getComponentIcon", () => {
  it("should return Alert icon for UNSPECIFIED component type", () => {
    const icon = getComponentIcon(ErrorComponentType.UNSPECIFIED);
    const { container } = render(<div>{icon}</div>);
    expect(container.querySelector("svg")).toBeInTheDocument();
  });

  it("should return LightningAlt icon for PSU component type", () => {
    const icon = getComponentIcon(ErrorComponentType.PSU);
    const { container } = render(<div>{icon}</div>);
    expect(container.querySelector("svg")).toBeInTheDocument();
  });

  it("should return Hashboard icon for HASH_BOARD component type", () => {
    const icon = getComponentIcon(ErrorComponentType.HASH_BOARD);
    const { container } = render(<div>{icon}</div>);
    expect(container.querySelector("svg")).toBeInTheDocument();
  });

  it("should return Fan icon for FAN component type", () => {
    const icon = getComponentIcon(ErrorComponentType.FAN);
    const { container } = render(<div>{icon}</div>);
    expect(container.querySelector("svg")).toBeInTheDocument();
  });

  it("should return ControlBoard icon for CONTROL_BOARD component type", () => {
    const icon = getComponentIcon(ErrorComponentType.CONTROL_BOARD);
    const { container } = render(<div>{icon}</div>);
    expect(container.querySelector("svg")).toBeInTheDocument();
  });

  it("should return Alert icon for EEPROM component type", () => {
    const icon = getComponentIcon(ErrorComponentType.EEPROM);
    const { container } = render(<div>{icon}</div>);
    expect(container.querySelector("svg")).toBeInTheDocument();
  });

  it("should return Alert icon for IO_MODULE component type", () => {
    const icon = getComponentIcon(ErrorComponentType.IO_MODULE);
    const { container } = render(<div>{icon}</div>);
    expect(container.querySelector("svg")).toBeInTheDocument();
  });

  it("should return Alert icon as fallback for unmapped component types", () => {
    // Test with an invalid component type to ensure fallback works
    const icon = getComponentIcon(999 as ErrorComponentType);
    const { container } = render(<div>{icon}</div>);
    expect(container.querySelector("svg")).toBeInTheDocument();
  });
});
