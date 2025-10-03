import { createContext, ReactNode, useMemo } from "react";
import { Api } from "@/protoOS/api/generatedApi";

const CreateApi = (baseUrl: string) => {
  const url = (baseUrl.length ? "/" : "") + baseUrl;
  const { api } = new Api({ baseUrl: url });

  // TODO: remove this when done with development
  (window as any).api = api;

  return api;
};

type ApiT = InstanceType<typeof Api>["api"];

type MinerHostingContextType = {
  api: ApiT | null;
  minerRoot: string;
  closeButton: ReactNode | null;
};

const MinerHostingContext = createContext<MinerHostingContextType>({
  api: null,
  minerRoot: "",
  closeButton: null,
});

type MinerHostingProviderProps = {
  children: ReactNode;
  baseUrl?: string;
  minerRoot?: string;
  closeButton?: ReactNode | null;
};

export const MinerHostingProvider = ({
  children,
  baseUrl = "",
  minerRoot = "",
  closeButton = null,
}: MinerHostingProviderProps) => {
  const api = useMemo(() => CreateApi(baseUrl), [baseUrl]);

  return (
    <MinerHostingContext.Provider value={{ api, minerRoot, closeButton }}>
      {children}
    </MinerHostingContext.Provider>
  );
};

export default MinerHostingContext;
