import { sessionOpenSchema } from "./contracts";

const assert = (condition: boolean, message: string): void => {
  if (!condition) {
    throw new Error(message);
  }
};

(() => {
  const parsed = sessionOpenSchema.safeParse({
    target: "local",
    sessionId: "550e8400-e29b-41d4-a716-446655440000"
  });

  assert(parsed.success, "sessionOpenSchema should accept local session payloads");
  if (!parsed.success) {
    return;
  }
  assert(parsed.data.target === "local", "sessionOpenSchema should keep local target");
})();

(() => {
  const parsed = sessionOpenSchema.safeParse({
    target: "remote",
    connectionId: "550e8400-e29b-41d4-a716-446655440001",
    sessionId: "550e8400-e29b-41d4-a716-446655440000"
  });

  assert(parsed.success, "sessionOpenSchema should accept remote session payloads");
})();

(() => {
  const parsed = sessionOpenSchema.safeParse({
    target: "local",
    connectionId: "550e8400-e29b-41d4-a716-446655440001"
  });

  assert(
    parsed.success === false,
    "sessionOpenSchema should reject connectionId on local session payloads"
  );
})();
