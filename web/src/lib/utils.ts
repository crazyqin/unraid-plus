import { type ClassValue, clsx } from 'clsx';
import { twMerge } from 'tailwind-merge';
import i18n from '@/i18n';

/** Merge Tailwind class names without conflicts. */
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

/** Format bytes as human-readable string. */
export function formatBytes(bytes: number, decimals = 1): string {
  if (!bytes || bytes === 0) return '0 B';
  if (bytes < 0) return '-' + formatBytes(-bytes, decimals);
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

/** Relative time like "3分钟前". */
export function timeAgo(ts: number): string {
  const diff = Date.now() - ts * 1000;
  const sec = Math.floor(diff / 1000);
  if (sec < 5) return i18n.t('time.justNow');
  if (sec < 60) return `${sec}${i18n.t('time.secondsAgo')}`;
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min}${i18n.t('time.minutesAgo')}`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `${hr}${i18n.t('time.hoursAgo')}`;
  const day = Math.floor(hr / 24);
  return `${day}${i18n.t('time.daysAgo')}`;
}

/** Truncate a long container name for display. */
export function truncate(s: string, n = 24): string {
  return s.length > n ? `${s.slice(0, n - 1)}…` : s;
}
