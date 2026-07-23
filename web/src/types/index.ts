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
  /** v0.8+: unique server ID from backend (host:port) */
  id?: string;
}

/** Server entry returned by GET /api/servers (v0.8+). */
export interface ServerInfo {
  id: string;
  host: string;
  port: number;
  user: string;
  authMode: string;
  label: string;
  connected: boolean;
  /** v0.3.1+: Whether SSH connection is available (terminal + SFTP). */
  sshAvailable: boolean;
  /** v0.3.1+: Whether Unraid WebGUI API session is active. */
  apiAvailable: boolean;
  lastSeen: string;
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

/** Per-container resource usage (mirrors docker_stats.go containerStats). */
export interface ContainerStats {
  id: string;
  name: string;
  cpuPct: number;
  memUsageBytes: number;
  memLimitBytes: number;
  memPct: number;
  netRxBytes: number;
  netTxBytes: number;
  blockReadBytes: number;
  blockWriteBytes: number;
  pids: number;
}

// Storage

/**
 * Structured SMART health data for a single physical disk.
 *
 * Mirrors server/internal/api/handler/smart.go smartInfo struct.
 * `available=false` (or `smart` being undefined on DiskInfo) means
 * smartctl is not installed, the device doesn't support SMART (md
 * software raid, USB bridges without SAT, loop, zfs vdevs), or the
 * JSON output failed to parse. In all those cases status='unknown'.
 */
export interface SmartInfo {
  /** Whether smartctl ran successfully and returned parseable JSON. */
  available: boolean;
  /** Mirrors smartctl's smart_status.passed bit. false = drive already failing. */
  passed: boolean;
  /** Curated status consumed by badges: ok | warning | failing | unknown. */
  status: 'ok' | 'warning' | 'failing' | 'unknown';
  /** On-disk sensor temperature (SATA attr 194 / NVMe composite). Undefined when not reported. */
  temperature?: number;
  /** SATA attr 5 raw value — reallocated sector count. */
  reallocated: number;
  /** SATA attr 197 raw value — current pending sector count. */
  pending: number;
  /** SATA attr 198 raw value — offline uncorrectable sectors. */
  uncorrectable: number;
  /** NVMe cumulative media & data integrity errors. */
  mediaErrors: number;
  modelName?: string;
  serialNumber?: string;
  /** Unix timestamp (seconds) of when this SMART entry was cached. */
  fetchedAt: number;
}

export interface DiskInfo {
  device: string;
  name: string;
  fsType: string;
  sizeBytes: number;
  usedBytes: number;
  /** On-disk temperature; populated from smart.temperature when available. */
  tempC?: number;
  readBytesPerSec: number;
  writeBytesPerSec: number;
  /** Sum of reallocated + pending + uncorrectable + media errors. */
  errors: number;
  /** Fill-ratio-based status (orthogonal to SMART health). */
  status: 'ok' | 'warning' | 'critical' | 'unknown';
  /** Structured SMART data; undefined when device doesn't support SMART or smartctl missing. */
  smart?: SmartInfo;
  /** Unraid slot name from disks.ini (e.g. "disk1", "parity", "cache1"). v0.7+ */
  diskName?: string;
  /** Unraid LED color indicator (e.g. "green-on", "yellow-on", "red-on", "grey-off"). v0.7+ */
  color?: string;
  /** "0" = SSD, "1" = HDD. From disks.ini rotational field. v0.7+ */
  rotational?: string;
  /** Transport type: "ata", "nvme", "usb". From disks.ini. v0.7+ */
  transport?: string;
}

export interface ArrayStatus {
  state: 'started' | 'stopped' | 'checking';
  disks: DiskInfo[];
  cacheDisks: DiskInfo[];
}

/**
 * Parity check progress (mirrors array.go parityStatusResp).
 *
 * GET /api/storage/parity-status returns this shape. When `state` is "idle",
 * all other fields are zero/empty. The UI polls this endpoint while a check
 * is running to update the progress bar.
 */
export interface ParityStatus {
  state: 'checking' | 'idle' | 'unknown';
  progress: number; // 0-100
  speed: string; // e.g. "152 MB/s"
  remaining: string; // e.g. "2h 15m"
  errors: number;
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
  /** v0.8+: unique server ID (host:port) for multi-server support. */
  serverId?: string;
  /** v0.3.1+: Whether SSH connection is available (terminal + SFTP). */
  sshAvailable: boolean;
  /** v0.3.1+: Whether Unraid WebGUI API session is active. */
  apiAvailable: boolean;
}
