import { useCallback } from "react";
import { useNavigate } from "react-router-dom";
import type { Container } from "@/protoFleet/api/generated/containers/v1/containers_pb";

export function containerDetailPath(container: Pick<Container, "deviceIdentifier">) {
  return `/containers/${encodeURIComponent(container.deviceIdentifier)}`;
}

export function useOpenContainerView() {
  const navigate = useNavigate();
  return useCallback((container: Container) => navigate(containerDetailPath(container)), [navigate]);
}

export function controllerLogsPath(container: Pick<Container, "deviceIdentifier">) {
  return `${containerDetailPath(container)}/controller-logs`;
}
