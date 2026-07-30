import { describe, expect, it } from "vitest";

import { getActivityIcon, getActivityIconTone } from "./activityIcons";
import {
  ArrowLeftCompact,
  Edit,
  InfoInverted,
  Lock,
  MiningPools,
  MinusFilled,
  Plus,
  PlusFilled,
  Settings,
  Speedometer,
} from "@/shared/assets/icons";

describe("activityIcons", () => {
  it("uses shared filled icons for create, delete, and logout events", () => {
    expect(getActivityIcon("create_pool")).toBe(PlusFilled);
    expect(getActivityIcon("create_user")).toBe(PlusFilled);
    expect(getActivityIcon("create_new_future_event")).toBe(PlusFilled);
    expect(getActivityIcon("building.created")).toBe(PlusFilled);
    expect(getActivityIcon("site.created")).toBe(PlusFilled);

    expect(getActivityIcon("delete_pool")).toBe(MinusFilled);
    expect(getActivityIcon("delete_role")).toBe(MinusFilled);
    expect(getActivityIcon("delete_new_future_event")).toBe(MinusFilled);
    expect(getActivityIcon("building.deleted")).toBe(MinusFilled);
    expect(getActivityIcon("site.deleted")).toBe(MinusFilled);
    expect(getActivityIcon("deactivate_user")).toBe(MinusFilled);

    expect(getActivityIcon("logout")).toBe(ArrowLeftCompact);
  });

  it("uses the stroked info icon as the fallback", () => {
    expect(getActivityIcon("unmapped_event")).toBe(InfoInverted);
  });

  it("uses the standard plus icon for add and assignment activity", () => {
    expect(getActivityIcon("add_devices")).toBe(Plus);
    expect(getActivityIcon("assign_devices_to_rack")).toBe(Plus);
    expect(getActivityIcon("building.rack_assigned")).toBe(Plus);
    expect(getActivityIcon("racks.assigned_to_site")).toBe(Plus);
    expect(getActivityIcon("devices.reassigned_to_site")).toBe(Plus);
  });

  it("uses the edit icon for generic save and update activity", () => {
    expect(getActivityIcon("save_rack")).toBe(Edit);
    expect(getActivityIcon("set_rack_slot")).toBe(Edit);
    expect(getActivityIcon("clear_rack_slot")).toBe(Edit);
    expect(getActivityIcon("update_collection")).toBe(Edit);
    expect(getActivityIcon("update_schedule")).toBe(Edit);
    expect(getActivityIcon("building.updated")).toBe(Edit);
    expect(getActivityIcon("site.updated")).toBe(Edit);
    expect(getActivityIcon("update_worker_names")).toBe(Edit);
  });

  it("keeps domain-specific update icons", () => {
    expect(getActivityIcon("update_password")).toBe(Lock);
    expect(getActivityIcon("update_pool")).toBe(MiningPools);
    expect(getActivityIcon("firmware_update")).toBe(Settings);
    expect(getActivityIcon("set_power_target")).toBe(Speedometer);
  });

  it("marks alert and destructive events as critical", () => {
    expect(getActivityIconTone("login_failed")).toBe("critical");
    expect(getActivityIconTone("delete_pool")).toBe("critical");
    expect(getActivityIconTone("building.deleted")).toBe("critical");
    expect(getActivityIconTone("remove_devices")).toBe("critical");
    expect(getActivityIconTone("revoke_api_key")).toBe("critical");
    expect(getActivityIconTone("unpair")).toBe("critical");
    expect(getActivityIconTone("login", "failure")).toBe("critical");
  });

  it("treats rack position changes like normal save activity", () => {
    expect(getActivityIconTone("set_rack_slot")).toBe("default");
    expect(getActivityIconTone("clear_rack_slot")).toBe("default");
  });
});
