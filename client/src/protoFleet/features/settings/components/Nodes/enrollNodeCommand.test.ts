import { describe, expect, it } from "vitest";
import { buildFleetNodeEnrollCommand } from "./enrollNodeCommand";

describe("buildFleetNodeEnrollCommand", () => {
  it.each([
    {
      label: "LAN HTTP",
      location: {
        origin: "http://192.168.1.20:8080",
        protocol: "http:",
        hostname: "192.168.1.20",
      },
      expected:
        "sudo -u fleetnode /opt/fleetnode/fleetnode --state-dir /var/lib/fleetnode enroll --server-url=http://192.168.1.20:4000 --allow-insecure-transport && sudo systemctl enable --now fleet-node.service",
    },
    {
      label: "HTTPS",
      location: {
        origin: "https://fleet.example.com",
        protocol: "https:",
        hostname: "fleet.example.com",
      },
      expected:
        "sudo -u fleetnode /opt/fleetnode/fleetnode --state-dir /var/lib/fleetnode enroll --server-url=https://fleet.example.com/api-proxy && sudo systemctl enable --now fleet-node.service",
    },
    {
      label: "localhost HTTP",
      location: {
        origin: "http://localhost:8080",
        protocol: "http:",
        hostname: "localhost",
      },
      expected:
        "sudo -u fleetnode /opt/fleetnode/fleetnode --state-dir /var/lib/fleetnode enroll --server-url=http://localhost:4000 && sudo systemctl enable --now fleet-node.service",
    },
    {
      label: "127.0.0.1 HTTP",
      location: {
        origin: "http://127.0.0.1:8080",
        protocol: "http:",
        hostname: "127.0.0.1",
      },
      expected:
        "sudo -u fleetnode /opt/fleetnode/fleetnode --state-dir /var/lib/fleetnode enroll --server-url=http://127.0.0.1:4000 && sudo systemctl enable --now fleet-node.service",
    },
    {
      label: "::1 HTTP",
      location: {
        origin: "http://[::1]:8080",
        protocol: "http:",
        hostname: "[::1]",
      },
      expected:
        "sudo -u fleetnode /opt/fleetnode/fleetnode --state-dir /var/lib/fleetnode enroll --server-url=http://[::1]:4000 && sudo systemctl enable --now fleet-node.service",
    },
  ])("builds the enroll command for $label", ({ location, expected }) => {
    expect(buildFleetNodeEnrollCommand(location)).toBe(expected);
  });
});
