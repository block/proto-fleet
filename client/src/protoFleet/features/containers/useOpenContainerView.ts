import { useCallback } from "react";
import { useNavigate } from "react-router-dom";
import type { Container } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import { rememberContainerMetadata } from "@/protoFleet/components/SingleMinerWrapper/routeState";

export function containerViewTarget(container: Container) {
  if (container.embeddedWebViewAvailable) {
    return { embedded: true, url: `/miners/${encodeURIComponent(container.deviceIdentifier)}` };
  }
  if (container.url) {
    try {
      const url = new URL(container.url);
      if (url.protocol === "http:" || url.protocol === "https:") {
        return { embedded: false, url: container.url };
      }
    } catch {
      return undefined;
    }
  }
  return undefined;
}

export function useOpenContainerView() {
  const navigate = useNavigate();
  return useCallback(
    (container: Container) => {
      const target = containerViewTarget(container);
      if (!target) return;
      if (target.embedded) {
        rememberContainerMetadata(container);
        navigate(target.url);
      } else {
        window.open(target.url, "_blank", "noopener,noreferrer");
      }
    },
    [navigate],
  );
}
