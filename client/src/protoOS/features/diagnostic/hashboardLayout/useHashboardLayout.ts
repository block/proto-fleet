import { LAYOUT_BY_MODULE_PROFILE } from "./layouts";
import type { HashboardLayout } from "./types";
import { useModuleProfile } from "@/protoOS/store";

/**
 * The layout descriptor for the connected device.
 *
 * Returns a module-level constant, so the reference is stable across renders
 * and every component below can call this directly rather than threading the
 * descriptor down as a prop.
 */
export const useHashboardLayout = (): HashboardLayout => LAYOUT_BY_MODULE_PROFILE[useModuleProfile()];
