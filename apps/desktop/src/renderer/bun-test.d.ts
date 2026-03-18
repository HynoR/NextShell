declare const Bun: {
  file(path: string | URL): {
    text(): Promise<string>;
  };
};

declare module "bun:test" {
  interface Matchers {
    toBe(expected: unknown): void;
    toEqual(expected: unknown): void;
    toContain(expected: unknown): void;
    toBeDefined(): void;
    toBeUndefined(): void;
    toHaveLength(expected: number): void;
    toHaveBeenCalled(): void;
    toHaveBeenCalledTimes(expected: number): void;
    toBeGreaterThan(expected: number): void;
    toBeInstanceOf(expected: new (...args: any[]) => any): void;
    toMatch(expected: RegExp | string): void;
    toMatchObject(expected: unknown): void;
    toThrow(expected?: RegExp | string | (new (...args: any[]) => any)): void;
    not: Matchers;
  }

  interface PromiseMatchers {
    toThrow(expected?: RegExp | string): Promise<void>;
    toBeInstanceOf(expected: new (...args: any[]) => any): Promise<void>;
  }

  interface Mock<T extends (...args: any[]) => any = (...args: any[]) => any> {
    (...args: Parameters<T>): ReturnType<T>;
    mockResolvedValueOnce(value: Awaited<ReturnType<T>>): Mock<T>;
    mockResolvedValue(value: Awaited<ReturnType<T>>): Mock<T>;
    mockImplementation(implementation: T): Mock<T>;
    mockReturnValue(value: ReturnType<T>): Mock<T>;
  }

  export function describe(name: string, fn: () => void): void;
  export function beforeEach(fn: () => void | Promise<void>): void;
  export function afterEach(fn: () => void | Promise<void>): void;
  export function test(name: string, fn: () => void | Promise<void>): void;
  export function mock<T extends (...args: any[]) => any>(fn?: T): Mock<T>;
  export function expect<T>(actual: T): Matchers & {
    rejects: PromiseMatchers;
  };
}
