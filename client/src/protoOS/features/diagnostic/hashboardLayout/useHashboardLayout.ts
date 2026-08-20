import { LAYOUT_BY_MODULE_MODEL } from "./layouts";
import type { HashboardLayout } from "./types";
import { useModuleModel } from "@/protoOS/store";

/**
 * The layout descriptor for the connected device.
 *
 * Returns a module-level constant, so the reference is stable across renders
 * and every component below can call this directly rather than threading the
 * descriptor down as a prop.
 */
export const useHashboardLayout = (): HashboardLayout => LAYOUT_BY_MODULE_MODEL[useModuleModel()];
