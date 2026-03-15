declare module "bun:test" {
  interface PromiseMatchers {
    toThrow(expected?: RegExp | string): Promise<void>;
  }

  export function describe(name: string, fn: () => void): void;
  export function test(name: string, fn: () => void | Promise<void>): void;
  export function expect<T>(actual: T): {
    toBe(expected: T): void;
    toEqual(expected: unknown): void;
    rejects: PromiseMatchers;
  };
}
