import {
  createContext,
  MutableRefObject,
  ReactNode,
  useRef,
  useState,
} from "react";

type PopoverContextType = {
  triggerRef: MutableRefObject<HTMLDivElement | null>;
  isTriggerFixed: boolean;
  setIsTriggerFixed: (isTriggerFixed: boolean) => void;
};

const PopoverContext = createContext<PopoverContextType | null>(null);

type PopoverProviderProps = {
  children: ReactNode;
};

export const PopoverProvider = ({ children }: PopoverProviderProps) => {
  const triggerRef = useRef<HTMLDivElement>(null);
  const [isTriggerFixed, setIsTriggerFixed] = useState(false);

  return (
    <PopoverContext.Provider
      value={{ triggerRef, isTriggerFixed, setIsTriggerFixed }}
    >
      {children}
    </PopoverContext.Provider>
  );
};

export default PopoverContext;
