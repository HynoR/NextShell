import { describe, expect, test } from "vitest";
import { LOCAL_DEFAULT_SCOPE_KEY } from "../../core/src/index";
import {
  filterResourcesByOriginScope,
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

  test("returns no resources when the target scope is unknown", () => {
    // 作用域切换器解析不出目标域时宁可空列表，也不能退回"显示全部"——那正是跨域误引用的入口。
    expect(
      filterResourcesByOriginScope([{ originScopeKey: LOCAL_DEFAULT_SCOPE_KEY }], undefined)
    ).toEqual([]);
  });
});
