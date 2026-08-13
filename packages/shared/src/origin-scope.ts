import { LOCAL_DEFAULT_SCOPE_KEY } from "../../core/src/index";

export const resolveOriginScopeKey = (resource: { originScopeKey?: string }): string =>
  resource.originScopeKey ?? LOCAL_DEFAULT_SCOPE_KEY;

export const resourceMatchesOriginScope = (
  resource: { originScopeKey?: string },
  targetScopeKey: string
): boolean => resolveOriginScopeKey(resource) === targetScopeKey;

export const filterResourcesByOriginScope = <T extends { originScopeKey?: string }>(
  resources: readonly T[],
  targetScopeKey: string | undefined
): T[] => {
  if (!targetScopeKey) {
    return [];
  }
  return resources.filter((resource) => resourceMatchesOriginScope(resource, targetScopeKey));
};
