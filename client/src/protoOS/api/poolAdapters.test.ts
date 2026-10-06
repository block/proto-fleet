import { describe, expect, it } from "vitest";

import { apiPoolToPoolInfo, poolInfoToPoolConfig, poolInfoToTestConnection } from "./poolAdapters";

describe("ProtoOS pool adapters", () => {
  it("preserves standalone SV2 authority keys from API responses", () => {
    expect(
      apiPoolToPoolInfo(
        {
          name: "Primary",
          priority: 0,
          url: "stratum2+tcp://pool.example.com",
          user: "worker",
          v2_authority_pubkey: "authority-key",
        },
        2,
      ),
    ).toEqual({
      name: "Primary",
      priority: 0,
      url: "stratum2+tcp://pool.example.com",
      username: "worker",
      password: "",
      v2_authority_pubkey: "authority-key",
    });
  });

  it("includes standalone SV2 authority keys in write and connection-test payloads", () => {
    const pool = {
      name: "Primary",
      priority: 0,
      url: "stratum2+tcp://pool.example.com",
      username: "worker",
      password: "secret",
      v2_authority_pubkey: "authority-key",
    };

    expect(poolInfoToPoolConfig(pool)).toEqual({
      name: "Primary",
      priority: 0,
      url: "stratum2+tcp://pool.example.com",
      username: "worker",
      password: "secret",
      v2_authority_pubkey: "authority-key",
    });
    expect(poolInfoToTestConnection(pool)).toEqual({
      url: "stratum2+tcp://pool.example.com",
      username: "worker",
      password: "secret",
      v2_authority_pubkey: "authority-key",
    });
  });

  it("clears authority keys from SV1 payloads", () => {
    expect(
      poolInfoToPoolConfig({
        name: "Primary",
        priority: 0,
        url: "stratum+tcp://pool.example.com",
        username: "worker",
        password: "",
        v2_authority_pubkey: "stale-key",
      }),
    ).toEqual({
      name: "Primary",
      priority: 0,
      url: "stratum+tcp://pool.example.com",
      username: "worker",
      password: "",
      v2_authority_pubkey: "",
    });
  });
});
