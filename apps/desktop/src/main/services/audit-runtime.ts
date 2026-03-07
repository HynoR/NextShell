export interface AuditRuntimeOptions {
  enabled: boolean;
  retentionDays: number;
}

export interface AuditRuntimeState {
  captureEnabled: boolean;
  runStartupPurge: boolean;
  runPeriodicPurge: boolean;
}

export const resolveAuditRuntime = (
  options: AuditRuntimeOptions
): AuditRuntimeState => {
  const hasRetention = options.retentionDays > 0;

  return {
    captureEnabled: options.enabled,
    runStartupPurge: hasRetention,
    runPeriodicPurge: options.enabled && hasRetention
  };
};
