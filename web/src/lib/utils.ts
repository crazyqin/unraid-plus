import { type ClassValue, clsx } from 'clsx';
import { twMerge } from 'tailwind-merge';

/** Merge Tailwind class names without conflicts. */
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

/** Format bytes as human-readable string. */
export function formatBytes(bytes: number, decimals = 1): string {
  if (bytes === 0 || bytes == null) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(decimals))} ${sizes[i]}`;
}

/** Format a number per second rate (bytes/s). */
export function formatRate(bytesPerSec: number): string {
  return `${formatBytes(bytesPerSec)}/s`;
}

/** Format a percentage with a fixed precision. */
export function formatPct(value: number, decimals = 1): string {
  return `${value.toFixed(decimals)}%`;
}

/** Format a Unix timestamp (seconds) to a short local string. */
export function formatTime(ts: number): string {
  return new Date(ts * 1000).toLocaleString();
}

/** Relative time like "3m ago". */
export function timeAgo(ts: number): string {
  const diff = Date.now() - ts * 1000;
  const sec = Math.floor(diff / 1000);
  if (sec < 60) return `${sec}s ago`;
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min}m ago`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `${hr}h ago`;
  const day = Math.floor(hr / 24);
  return `${day}d ago`;
}

/** Truncate a long container name for display. */
export function truncate(s: string, n = 24): string {
  return s.length > n ? `${s.slice(0, n - 1)}…` : s;
}
