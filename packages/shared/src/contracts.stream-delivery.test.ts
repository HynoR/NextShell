import {
  sessionDataEventSchema,
  streamDeliveryAckSchema,
} from "./contracts";

function assert(condition: boolean, message: string): void {
  if (!condition) {
    throw new Error(message);
  }
}

(() => {
  const parsed = sessionDataEventSchema.safeParse({
    sessionId: "67c5901f-6793-4dbd-b98d-650ef2a385db",
    data: "hello",
    deliveryId: 1,
    byteLength: 5,
  });

  assert(parsed.success, "sessionDataEventSchema should require delivery metadata");
})();

(() => {
  const parsed = streamDeliveryAckSchema.safeParse({
    streamKind: "session",
    streamId: "session-1",
    deliveryId: 3,
    consumedBytes: 1024,
  });

  assert(parsed.success, "streamDeliveryAckSchema should accept session acknowledgements");
})();

(() => {
  const parsed = streamDeliveryAckSchema.safeParse({
    streamKind: "session",
    streamId: "session-1",
    deliveryId: 3,
  });

  assert(parsed.success === false, "session ack should require consumedBytes");
})();

(() => {
  const parsed = streamDeliveryAckSchema.safeParse({
    streamKind: "monitor-system",
    streamId: "connection-1",
    deliveryId: 9,
  });

  assert(parsed.success, "monitor ack should not require consumedBytes");
})();
