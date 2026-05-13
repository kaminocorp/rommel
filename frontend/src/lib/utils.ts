import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}

export function invariant(cond: unknown, msg: string): asserts cond {
  if (!cond) throw new Error(`invariant: ${msg}`);
}
