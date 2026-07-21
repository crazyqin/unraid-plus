/** Shared API types — kept in sync with server/internal/api/handler DTOs. */

export type ServerStatus =
  | 'connected'
  | 'disconnected'
  | 'connecting'
  | 'error';

/** Top-level server config (Unraid host + auth mode). */
export interface ServerConfig {
  host: string;
  /** HTTPS-aware API base, e.g. `https://tower.local` */
  apiBase?: string;
  sshPort: number;
  user: string;
  /** 'password' = zero-config mode; 'key' = explicit key pair */
  authMode: 'password' | 'key';
  /** Whether the server holds a key pair (managed automatically). */
  hasKeyPair?: boolean;
  status: ServerStatus;
  label?: string;
}

export interface HealthSummary {
  ok: boolean;
  version: string;
  uptime: number;
}

// Dashboard
export interface CpuInfo {
  modelName: string;
  cores: number;
  usagePct: number;
  perCoreUsagePct: number[];
  perCoreTempC: number[];
}

export interface MemoryInfo {
  totalBytes: number;
  usedBytes: number;
  cacheBytes: number;
  usagePct: number;
}

export interface NetworkInfo {
  iface: string;
  rxBytesPerSec: number;
  txBytesPerSec: number;
  rxTotalBytes: number;
  txTotalBytes: number;
}

export interface DashboardSummary {
  cpu: CpuInfo;
  memory: MemoryInfo;
  network: NetworkInfo[];
  arrayRwBytesPerSec: { read: number; write: number };
  uptime: number;
  loadAvg: [number, number, number];
}

// Docker
export interface DockerContainer {
  id: string;
  name: string;
  image: string;
  status: 'running' | 'exited' | 'paused' | 'restarting' | 'created' | 'dead';
  state: string;
  createdAt: number;
  startedAt?: number;
  ports: string[];
  mounts: { source: string; destination: string; mode: string }[];
}

// Storage
export interface DiskInfo {
  device: string;
  name: string;
  fsType: string;
  sizeBytes: number;
  usedBytes: number;
  tempC?: number;
  readBytesPerSec: number;
  writeBytesPerSec: number;
  errors: number;
  status: 'ok' | 'warning' | 'critical' | 'unknown';
}

export interface ArrayStatus {
  state: 'started' | 'stopped' | 'checking';
  disks: DiskInfo[];
  cacheDisks: DiskInfo[];
}

// Files
export interface FileEntry {
  name: string;
  path: string;
  isDir: boolean;
  sizeBytes: number;
  modTime: number;
  mode: string;
  owner: string;
  group: string;
}

export interface ListFilesResponse {
  path: string;
  entries: FileEntry[];
}

// VMs
export interface VmInfo {
  id: string;
  name: string;
  status: 'running' | 'shutoff' | 'paused' | 'unknown';
  vcpus: number;
  memoryBytes: number;
  autostart: boolean;
}

// Onboarding / connection test
export interface ConnectRequest {
  host: string;
  apiBase?: string;
  sshPort: number;
  user: string;
  password?: string;
  /** Explicit private key (PEM) when authMode == 'key'. */
  privateKey?: string;
  passphrase?: string;
}

export interface ConnectResult {
  ok: boolean;
  message: string;
  /** Fingerprint of the server SSH host key, to confirm trust. */
  hostFingerprint?: string;
  serverVersion?: string;
}
