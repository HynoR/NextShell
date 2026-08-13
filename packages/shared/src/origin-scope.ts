import { buildScopeKey, LOCAL_DEFAULT_SCOPE_KEY } from "../../core/src/index";
import { CONNECTION_ZONES } from "./constants";

export const resolveOriginScopeKey = (resource: { originScopeKey?: string }): string =>
  resource.originScopeKey ?? LOCAL_DEFAULT_SCOPE_KEY;

export const resourceMatchesOriginScope = (
  resource: { originScopeKey?: string },
  targetScopeKey: string
): boolean => resolveOriginScopeKey(resource) === targetScopeKey;

export const resolveFormTargetScopeKey = (input: {
  groupZone?: string;
  workspace?: { apiBaseUrl: string; workspaceName: string };
}): string | undefined => {
  if (input.groupZone === CONNECTION_ZONES.WORKSPACE) {
    if (!input.workspace) {
      return undefined;
    }
    return buildScopeKey({
      kind: "cloud",
      apiBaseUrl: input.workspace.apiBaseUrl,
      workspaceName: input.workspace.workspaceName
    });
  }
  return LOCAL_DEFAULT_SCOPE_KEY;
};

export const filterResourcesByOriginScope = <T extends { originScopeKey?: string }>(
  resources: readonly T[],
  targetScopeKey: string | undefined
): T[] => {
  if (!targetScopeKey) {
    return [];
  }
  return resources.filter((resource) => resourceMatchesOriginScope(resource, targetScopeKey));
};
