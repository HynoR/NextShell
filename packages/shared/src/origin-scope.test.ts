import { describe, expect, test } from "vitest";
import { LOCAL_DEFAULT_SCOPE_KEY } from "../../core/src/index";
import { CONNECTION_ZONES } from "./constants";
import {
  filterResourcesByOriginScope,
  resolveFormTargetScopeKey,
  resolveOriginScopeKey,
  resourceMatchesOriginScope
} from "./origin-scope";

describe("origin scope matching", () => {
  test("treats a missing originScopeKey as the local default", () => {
    expect(resolveOriginScopeKey({})).toBe(LOCAL_DEFAULT_SCOPE_KEY);
    expect(resourceMatchesOriginScope({}, LOCAL_DEFAULT_SCOPE_KEY)).toBe(true);
    expect(resourceMatchesOriginScope({ originScopeKey: "cloud-a" }, LOCAL_DEFAULT_SCOPE_KEY)).toBe(
      false
    );
  });

  test("filters keys by originScopeKey rather than originKind", () => {
    const localKey = {
      id: "local",
      originKind: "local" as const,
      originScopeKey: LOCAL_DEFAULT_SCOPE_KEY
    };
    const copiedIntoLocal = {
      id: "copied",
      originKind: "cloud" as const,
      originScopeKey: LOCAL_DEFAULT_SCOPE_KEY,
      originWorkspaceId: "workspace-a"
    };
    const cloudKey = {
      id: "cloud",
      originKind: "cloud" as const,
      originScopeKey: "sync.example.com-team-a",
      originWorkspaceId: "workspace-a"
    };

    expect(
      filterResourcesByOriginScope([localKey, copiedIntoLocal, cloudKey], LOCAL_DEFAULT_SCOPE_KEY)
    ).toEqual([localKey, copiedIntoLocal]);
    expect(
      filterResourcesByOriginScope(
        [localKey, copiedIntoLocal, cloudKey],
        "sync.example.com-team-a"
      ).map((item) => item.id)
    ).toEqual(["cloud"]);
  });

  test("returns no resources when the target workspace scope cannot be resolved", () => {
    expect(
      filterResourcesByOriginScope([{ originScopeKey: LOCAL_DEFAULT_SCOPE_KEY }], undefined)
    ).toEqual([]);
    expect(resolveFormTargetScopeKey({ groupZone: CONNECTION_ZONES.WORKSPACE })).toBeUndefined();
  });

  test("computes the same cloud scope key the main process uses", () => {
    expect(
      resolveFormTargetScopeKey({
        groupZone: CONNECTION_ZONES.WORKSPACE,
        workspace: {
          apiBaseUrl: "https://sync.example.com/",
          workspaceName: "team-a"
        }
      })
    ).toBe("sync.example.com-team-a");
    expect(resolveFormTargetScopeKey({ groupZone: CONNECTION_ZONES.SERVER })).toBe(
      LOCAL_DEFAULT_SCOPE_KEY
    );
  });
});
