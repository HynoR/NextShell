export async function persistConnectionWrite<T>(
  write: () => Promise<T>,
  recover: () => void | Promise<void>
): Promise<T> {
  try {
    return await write();
  } catch (error) {
    await recover();
    throw error;
  }
}
