import {
  resolveDeviceKey,
  type DeviceKeyDbAccess,
  type DeviceKeyStore
} from "../../../../../packages/security/src/index";
import { logger } from "../logger";

export type DeviceKeyStatus = "unresolved" | "database";

export interface DeviceKeyProviderOptions {
  store: DeviceKeyStore;
  db: DeviceKeyDbAccess;
}

/** Resolve the device key lazily and keep the authoritative copy in SQLite. */
export class DeviceKeyProvider {
  private resolved: Buffer | undefined;
  private inFlight: Promise<Buffer> | undefined;
  private status: DeviceKeyStatus = "unresolved";

  constructor(private readonly options: DeviceKeyProviderOptions) {}

  getStatus(): DeviceKeyStatus {
    return this.status;
  }

  async get(): Promise<Buffer> {
    if (this.resolved) {
      return this.resolved;
    }
    this.inFlight ??= this.resolve();
    return this.inFlight;
  }

  private async resolve(): Promise<Buffer> {
    try {
      const result = await resolveDeviceKey(this.options.store, this.options.db);
      this.resolved = Buffer.from(result.deviceKeyHex, "hex");
      this.status = result.storedIn;
      logger.info("[Security] device key stored in local database");
      return this.resolved;
    } finally {
      this.inFlight = undefined;
    }
  }
}
