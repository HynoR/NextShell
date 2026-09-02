/**
 * Minimal ZIP writer for the `.mcpb` export (an MCPB bundle is a plain ZIP).
 *
 * Entries are stored uncompressed: the archive holds one already-minified
 * bridge bundle plus a small manifest, so deflate would buy little and cost a
 * dependency. The desktop app ships no zip library and this stays that way.
 */

const LOCAL_FILE_HEADER_SIG = 0x04034b50;
const CENTRAL_DIR_HEADER_SIG = 0x02014b50;
const END_OF_CENTRAL_DIR_SIG = 0x06054b50;
/** General-purpose bit 11: entry names are UTF-8. */
const UTF8_FLAG = 0x0800;
/** "Version made by": unix (3) so external attributes carry the file mode. */
const VERSION_MADE_BY = (3 << 8) | 20;
const VERSION_NEEDED = 20;

const CRC_TABLE: Uint32Array = (() => {
  const table = new Uint32Array(256);
  for (let n = 0; n < 256; n++) {
    let c = n;
    for (let k = 0; k < 8; k++) {
      c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
    }
    table[n] = c >>> 0;
  }
  return table;
})();

export const crc32 = (data: Buffer): number => {
  let crc = 0xffffffff;
  for (let i = 0; i < data.length; i++) {
    crc = (CRC_TABLE[(crc ^ data[i]!) & 0xff]! ^ (crc >>> 8)) >>> 0;
  }
  return (crc ^ 0xffffffff) >>> 0;
};

const toDosDateTime = (date: Date): { time: number; date: number } => ({
  time: (date.getHours() << 11) | (date.getMinutes() << 5) | Math.floor(date.getSeconds() / 2),
  date:
    (Math.max(0, date.getFullYear() - 1980) << 9) | ((date.getMonth() + 1) << 5) | date.getDate()
});

export interface ZipEntry {
  /** Forward-slash relative path inside the archive, e.g. `server/index.js`. */
  name: string;
  data: Buffer;
  /** Unix permission bits; defaults to 0o644. */
  mode?: number;
}

export const createZipArchive = (entries: ZipEntry[], now: Date = new Date()): Buffer => {
  const { time, date } = toDosDateTime(now);
  const chunks: Buffer[] = [];
  const central: Buffer[] = [];
  let offset = 0;

  for (const entry of entries) {
    const name = Buffer.from(entry.name, "utf8");
    const checksum = crc32(entry.data);
    const mode = entry.mode ?? 0o644;

    const local = Buffer.alloc(30);
    local.writeUInt32LE(LOCAL_FILE_HEADER_SIG, 0);
    local.writeUInt16LE(VERSION_NEEDED, 4);
    local.writeUInt16LE(UTF8_FLAG, 6);
    local.writeUInt16LE(0, 8); // method: store
    local.writeUInt16LE(time, 10);
    local.writeUInt16LE(date, 12);
    local.writeUInt32LE(checksum, 14);
    local.writeUInt32LE(entry.data.length, 18);
    local.writeUInt32LE(entry.data.length, 22);
    local.writeUInt16LE(name.length, 26);
    local.writeUInt16LE(0, 28); // extra length

    const header = Buffer.alloc(46);
    header.writeUInt32LE(CENTRAL_DIR_HEADER_SIG, 0);
    header.writeUInt16LE(VERSION_MADE_BY, 4);
    header.writeUInt16LE(VERSION_NEEDED, 6);
    header.writeUInt16LE(UTF8_FLAG, 8);
    header.writeUInt16LE(0, 10); // method: store
    header.writeUInt16LE(time, 12);
    header.writeUInt16LE(date, 14);
    header.writeUInt32LE(checksum, 16);
    header.writeUInt32LE(entry.data.length, 20);
    header.writeUInt32LE(entry.data.length, 24);
    header.writeUInt16LE(name.length, 28);
    // extra / comment / disk / internal attrs stay zero
    header.writeUInt32LE(((0o100000 | mode) << 16) >>> 0, 38); // external attrs
    header.writeUInt32LE(offset, 42);
    central.push(Buffer.concat([header, name]));

    chunks.push(local, name, entry.data);
    offset += local.length + name.length + entry.data.length;
  }

  const centralStart = offset;
  const centralBuf = Buffer.concat(central);

  const eocd = Buffer.alloc(22);
  eocd.writeUInt32LE(END_OF_CENTRAL_DIR_SIG, 0);
  eocd.writeUInt16LE(entries.length, 8);
  eocd.writeUInt16LE(entries.length, 10);
  eocd.writeUInt32LE(centralBuf.length, 12);
  eocd.writeUInt32LE(centralStart, 16);

  return Buffer.concat([...chunks, centralBuf, eocd]);
};
