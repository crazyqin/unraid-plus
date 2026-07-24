import { useEffect, useMemo, useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { motion } from 'framer-motion';
import {
  ChevronRight,
  ChevronUp,
  ChevronDown,
  Download,
  Eye,
  File as FileIcon,
  Folder,
  FolderPlus,
  FolderTree,
  Home,
  Loader2,
  Pencil,
  Save,
  Trash2,
  Upload,
} from 'lucide-react';
import { api, ApiError } from '@/lib/api';
import hljs from '@/lib/highlight';
import '@/lib/highlight.css';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Progress } from '@/components/ui/progress';
import { Badge } from '@/components/ui/badge';
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { ConfirmDialog } from '@/components/ui/alert-dialog';
import { formatBytes, timeAgo, cn } from '@/lib/utils';
import type { FileEntry, ListFilesResponse } from '@/types';
import { springGentle } from '@/lib/motion';
import { PageHeader, PageOrb, PageShell } from '@/components/layout/PageShell';

type SortKey = 'name' | 'sizeBytes' | 'modTime';
type SortDir = 'asc' | 'desc';

export default function FilesPage() {
  const { t } = useTranslation();
  const [path, setPath] = useState('/mnt');
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [uploadProgress, setUploadProgress] = useState<number | null>(null);
  const [dragOver, setDragOver] = useState(false);
  const [renameTarget, setRenameTarget] = useState<FileEntry | null>(null);
  const [mkdirOpen, setMkdirOpen] = useState(false);
  const [previewTarget, setPreviewTarget] = useState<FileEntry | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [uploadError, setUploadError] = useState<string | null>(null);
  const [sortKey, setSortKey] = useState<SortKey>('name');
  const [sortDir, setSortDir] = useState<SortDir>('asc');
  const fileInputRef = useRef<HTMLInputElement>(null);
  const qc = useQueryClient();

  const { data, isLoading, isError } = useQuery({
    queryKey: ['files', path],
    queryFn: () => api.get<ListFilesResponse>(`/files?path=${encodeURIComponent(path)}`),
  });

  const breadcrumbs = path.split('/').filter(Boolean);

  const enter = (e: FileEntry) => {
    if (e.isDir) {
      setSelected(new Set());
      setPath(e.path);
    }
  };

  const goto = (idx: number) => {
    setSelected(new Set());
    const next = '/' + breadcrumbs.slice(0, idx + 1).join('/');
    // Navigate to /mnt if path resolves to root (not in backend whitelist)
    setPath(next && next !== '/' ? next : '/mnt');
  };

  const toggle = (p: string) =>
    setSelected((s) => {
      const n = new Set(s);
      n.has(p) ? n.delete(p) : n.add(p);
      return n;
    });

  const del = async () => {
    if (selected.size === 0) return;
    try {
      await api.post('/files/delete', { paths: [...selected] });
      setSelected(new Set());
      qc.invalidateQueries({ queryKey: ['files', path] });
    } catch (err) {
      setUploadError(err instanceof ApiError ? err.message : t('files.deleteFailed'));
    } finally {
      setConfirmDelete(false);
    }
  };

  const download = () => {
    if (selected.size !== 1) return;
    const p = [...selected][0];
    window.location.href = api.downloadUrl(
      `/files/download?path=${encodeURIComponent(p)}`,
    );
  };

  const upload = async (files: FileList) => {
    if (files.length === 0) return;
    setUploadProgress(0);
    setUploadError(null);
    try {
      const formData = new FormData();
      for (const f of files) {
        formData.append('files', f);
      }
      await api.uploadWithProgress(
        `/files/upload?dir=${encodeURIComponent(path)}`,
        formData,
        (loaded, total) => setUploadProgress(Math.round((loaded / total) * 100)),
      );
      qc.invalidateQueries({ queryKey: ['files', path] });
    } catch (err) {
      setUploadError(err instanceof ApiError ? err.message : t('files.uploadFailed'));
    } finally {
      setUploadProgress(null);
      if (fileInputRef.current) fileInputRef.current.value = '';
    }
  };

  const selectedEntries = (data?.entries ?? []).filter((e) =>
    selected.has(e.path),
  );

  const toggleSort = (key: SortKey) => {
    if (sortKey === key) {
      setSortDir((d) => (d === 'asc' ? 'desc' : 'asc'));
    } else {
      setSortKey(key);
      setSortDir('asc');
    }
  };

  const sortedEntries = useMemo(() => {
    const entries = data?.entries ?? [];
    // Always put directories first
    const dirs = entries.filter((e) => e.isDir);
    const files = entries.filter((e) => !e.isDir);

    const sorter = (a: FileEntry, b: FileEntry) => {
      let cmp = 0;
      if (sortKey === 'name') cmp = a.name.localeCompare(b.name);
      else if (sortKey === 'sizeBytes') cmp = a.sizeBytes > b.sizeBytes ? 1 : a.sizeBytes < b.sizeBytes ? -1 : 0;
      else cmp = a.modTime > b.modTime ? 1 : a.modTime < b.modTime ? -1 : 0;
      return sortDir === 'asc' ? cmp : -cmp;
    };

    dirs.sort(sorter);
    files.sort(sorter);
    return [...dirs, ...files];
  }, [data?.entries, sortKey, sortDir]);

  const SortIcon = ({ column }: { column: SortKey }) => {
    if (sortKey !== column) {
      return <ChevronUp className="h-3 w-3 opacity-0 group-hover:opacity-40" />;
    }
    return sortDir === 'asc'
      ? <ChevronUp className="h-3 w-3" />
      : <ChevronDown className="h-3 w-3" />;
  };

  const entryCount = sortedEntries.length;
  const dirCount = sortedEntries.filter((e) => e.isDir).length;

  return (
    <PageShell className="flex h-full min-h-0 flex-col">
      <PageOrb className="-left-16 -top-20 bg-amber-500/10" />

      {/* Hidden file input for uploads */}
      <input
        ref={fileInputRef}
        type="file"
        multiple
        className="hidden"
        onChange={(e) => e.target.files && upload(e.target.files)}
      />

      <PageHeader
        eyebrow={
          <span className="inline-flex items-center gap-2">
            <FolderTree className="h-3 w-3 text-primary" />
            SFTP
          </span>
        }
        title={t('files.title')}
        meta={
          <>
            <Badge
              variant="secondary"
              className="max-w-[min(100%,28rem)] truncate rounded-full px-2.5 font-mono-data text-[11px]"
              title={path}
            >
              {path}
            </Badge>
            <Badge variant="outline" className="rounded-full px-2.5 text-[11px]">
              {entryCount} {t('files.itemCount')}
              {dirCount > 0 ? ` · ${dirCount} ${t('files.folderCount')}` : ''}
            </Badge>
            {selected.size > 0 && (
              <Badge
                variant="default"
                className="rounded-full px-2.5 text-[11px] font-semibold tracking-wide"
              >
                {selected.size} {t('files.selectedCount')}
              </Badge>
            )}
          </>
        }
        actions={
          <div className="flex flex-wrap items-center gap-2">
            <Button
              size="sm"
              variant="outline"
              className="h-9 rounded-xl"
              disabled={uploadProgress !== null}
              onClick={() => fileInputRef.current?.click()}
            >
              {uploadProgress !== null ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <Upload className="h-3.5 w-3.5" />
              )}
              {t('files.upload')}
            </Button>
            <Button
              size="sm"
              variant="outline"
              className="h-9 rounded-xl"
              onClick={() => setMkdirOpen(true)}
            >
              <FolderPlus className="h-3.5 w-3.5" /> {t('files.newFolder')}
            </Button>
            <Button
              size="sm"
              variant="outline"
              className="h-9 rounded-xl"
              disabled={selected.size === 0}
              onClick={() => setConfirmDelete(true)}
            >
              <Trash2 className="h-3.5 w-3.5" /> {t('files.delete')}
            </Button>
            <Button
              size="sm"
              variant="outline"
              className="h-9 rounded-xl"
              disabled={selected.size !== 1 || selectedEntries[0]?.isDir}
              onClick={download}
              title={
                selected.size === 1 && selectedEntries[0]?.isDir
                  ? t('files.cannotDownloadDir')
                  : ''
              }
            >
              <Download className="h-3.5 w-3.5" /> {t('files.download')}
            </Button>
            <Button
              size="sm"
              variant="outline"
              className="h-9 rounded-xl"
              disabled={selected.size !== 1}
              onClick={() => selectedEntries[0] && setRenameTarget(selectedEntries[0])}
            >
              <Pencil className="h-3.5 w-3.5" /> {t('files.rename')}
            </Button>
            <Button
              size="sm"
              variant="outline"
              className="h-9 rounded-xl"
              disabled={selected.size !== 1 || selectedEntries[0]?.isDir}
              onClick={() => selectedEntries[0] && setPreviewTarget(selectedEntries[0])}
            >
              <Eye className="h-3.5 w-3.5" /> {t('files.preview')}
            </Button>
          </div>
        }
      />

      {/* Breadcrumb bar */}
      <motion.div
        className="card-bento flex flex-wrap items-center gap-0.5 px-3 py-2.5 text-sm"
        initial={{ opacity: 0, y: 8 }}
        animate={{ opacity: 1, y: 0 }}
        transition={springGentle}
      >
        <button
          type="button"
          onClick={() => goto(-1)}
          className="inline-flex items-center rounded-lg p-1.5 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
          title="/mnt"
        >
          <Home className="h-3.5 w-3.5" />
        </button>
        {breadcrumbs.map((seg, i) => (
          <div key={`${seg}-${i}`} className="flex min-w-0 items-center">
            <ChevronRight className="mx-0.5 h-3 w-3 shrink-0 text-muted-foreground/50" />
            <button
              type="button"
              onClick={() => goto(i)}
              className={cn(
                'max-w-[10rem] truncate rounded-lg px-2 py-1 transition-colors hover:bg-accent',
                i === breadcrumbs.length - 1
                  ? 'font-medium text-foreground'
                  : 'text-muted-foreground hover:text-foreground',
              )}
            >
              {seg}
            </button>
          </div>
        ))}
      </motion.div>

      {/* Upload progress */}
      {uploadProgress !== null && (
        <motion.div
          className="flex items-center gap-3 rounded-2xl border border-border/40 bg-card/40 p-3 glass"
          initial={{ opacity: 0, y: -6 }}
          animate={{ opacity: 1, y: 0 }}
        >
          <span className="shrink-0 text-xs text-muted-foreground">{t('files.uploading')}</span>
          <Progress className="flex-1" value={uploadProgress} />
          <span className="shrink-0 font-mono-data text-xs tabular-nums text-muted-foreground">
            {uploadProgress}%
          </span>
        </motion.div>
      )}

      {/* Upload error */}
      {uploadError && (
        <motion.div
          className="flex items-center justify-between rounded-2xl border border-destructive/30 bg-destructive/5 p-4 text-sm text-destructive glass"
          initial={{ opacity: 0, y: -8 }}
          animate={{ opacity: 1, y: 0 }}
        >
          <span>{uploadError}</span>
          <button className="text-xs underline" onClick={() => setUploadError(null)}>
            {t('common.close')}
          </button>
        </motion.div>
      )}

      {/* File table */}
      <div
        className="card-bento relative min-h-0 flex-1 overflow-hidden"
        onDragOver={(e) => {
          e.preventDefault();
          setDragOver(true);
        }}
        onDragLeave={() => setDragOver(false)}
        onDrop={(e) => {
          e.preventDefault();
          setDragOver(false);
          if (e.dataTransfer.files.length > 0) upload(e.dataTransfer.files);
        }}
      >
        {isLoading ? (
          <div className="flex h-full min-h-[16rem] items-center justify-center gap-2 text-sm text-muted-foreground">
            <Loader2 className="h-4 w-4 animate-spin" /> {t('files.readingDir')}
          </div>
        ) : isError ? (
          <div className="flex h-full min-h-[16rem] flex-col items-center justify-center gap-3 text-sm text-muted-foreground">
            <FolderTree className="h-10 w-10 text-muted-foreground/30" />
            <span className="text-destructive">{t('files.cannotReadDir')}</span>
          </div>
        ) : (
          <div className="h-full min-h-[16rem] overflow-auto">
            <table className="w-full text-sm">
              <thead className="sticky top-0 z-[1] bg-card/90 text-xs text-muted-foreground backdrop-blur-md">
                <tr className="border-b border-border/50">
                  <th className="px-4 py-3 text-left font-medium">
                    <button
                      type="button"
                      className="group inline-flex items-center gap-1 hover:text-foreground"
                      onClick={() => toggleSort('name')}
                    >
                      {t('files.name')} <SortIcon column="name" />
                    </button>
                  </th>
                  <th className="px-4 py-3 text-right font-medium">
                    <button
                      type="button"
                      className="group inline-flex items-center gap-1 hover:text-foreground"
                      onClick={() => toggleSort('sizeBytes')}
                    >
                      {t('files.size')} <SortIcon column="sizeBytes" />
                    </button>
                  </th>
                  <th className="px-4 py-3 text-right font-medium">
                    <button
                      type="button"
                      className="group inline-flex items-center gap-1 hover:text-foreground"
                      onClick={() => toggleSort('modTime')}
                    >
                      {t('files.modified')} <SortIcon column="modTime" />
                    </button>
                  </th>
                </tr>
              </thead>
              <tbody>
                {sortedEntries.map((e) => (
                  <tr
                    key={e.path}
                    onClick={() => (e.isDir ? enter(e) : toggle(e.path))}
                    onDoubleClick={() => !e.isDir && setPreviewTarget(e)}
                    className={cn(
                      'cursor-pointer border-b border-border/40 transition-colors hover:bg-accent/40',
                      selected.has(e.path) && 'bg-primary/10 hover:bg-primary/15',
                    )}
                  >
                    <td className="px-4 py-2.5">
                      <div className="flex min-w-0 items-center gap-2.5">
                        {e.isDir ? (
                          <Folder className="h-4 w-4 shrink-0 text-amber-500" />
                        ) : (
                          <FileIcon className="h-4 w-4 shrink-0 text-muted-foreground" />
                        )}
                        <span className="truncate font-medium">{e.name}</span>
                      </div>
                    </td>
                    <td className="px-4 py-2.5 text-right font-mono-data tabular-nums text-muted-foreground">
                      {e.isDir ? '—' : formatBytes(e.sizeBytes)}
                    </td>
                    <td className="px-4 py-2.5 text-right tabular-nums text-muted-foreground">
                      {timeAgo(e.modTime)}
                    </td>
                  </tr>
                ))}
                {sortedEntries.length === 0 && (
                  <tr>
                    <td colSpan={3} className="px-4 py-16 text-center">
                      <div className="flex flex-col items-center gap-3 text-sm text-muted-foreground">
                        <Folder className="h-10 w-10 text-muted-foreground/25" />
                        {t('files.emptyDir')}
                      </div>
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        )}

        {/* Drag-and-drop overlay */}
        {dragOver && (
          <div className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center rounded-[inherit] border-2 border-dashed border-primary bg-primary/10 backdrop-blur-sm">
            <div className="flex flex-col items-center gap-2 text-primary">
              <Upload className="h-8 w-8" />
              <span className="text-sm font-medium">{t('files.dropToUpload')}</span>
            </div>
          </div>
        )}
      </div>

      <RenameDialog
        target={renameTarget}
        onClose={() => setRenameTarget(null)}
        onDone={() => qc.invalidateQueries({ queryKey: ['files', path] })}
      />
      <MkdirDialog
        open={mkdirOpen}
        basePath={path}
        onClose={() => setMkdirOpen(false)}
        onDone={() => qc.invalidateQueries({ queryKey: ['files', path] })}
      />
      <PreviewDialog
        target={previewTarget}
        onClose={() => setPreviewTarget(null)}
      />
      <ConfirmDialog
        open={confirmDelete}
        title={t('files.confirmDeleteTitle')}
        description={t('files.confirmDeleteDesc', { count: selected.size })}
        confirmText={t('files.delete')}
        variant="destructive"
        onConfirm={del}
        onCancel={() => setConfirmDelete(false)}
      />
    </PageShell>
  );
}

/* ------------------------------ Rename Dialog ----------------------------- */

function RenameDialog({
  target,
  onClose,
  onDone,
}: {
  target: FileEntry | null;
  onClose: () => void;
  onDone: () => void;
}) {
  const { t } = useTranslation();
  const [newName, setNewName] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  // Sync input when target changes
  const targetName = target?.name ?? '';
  const setInput = () => {
    setNewName(targetName);
    setError('');
  };

  const submit = async () => {
    if (!target || !newName || newName === targetName) {
      onClose();
      return;
    }
    const dir = target.path.substring(0, target.path.lastIndexOf('/'));
    const newPath = `${dir}/${newName}`;
    setLoading(true);
    setError('');
    try {
      await api.post('/files/rename', { oldPath: target.path, newPath });
      onDone();
      onClose();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : t('files.renameFailed'));
    } finally {
      setLoading(false);
    }
  };

  return (
    <Dialog
      open={!!target}
      onOpenChange={(o) => {
        if (o) setInput();
        else onClose();
      }}
    >
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle>{t('files.renameTitle')}</DialogTitle>
        </DialogHeader>
        <div className="space-y-2">
          <Label htmlFor="rename-input">{t('files.newName')}</Label>
          <Input
            id="rename-input"
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
            autoFocus
            onKeyDown={(e) => e.key === 'Enter' && submit()}
          />
          {error && <p className="text-sm text-destructive">{error}</p>}
        </div>
        <DialogFooter>
          <Button variant="outline" className="rounded-lg" onClick={onClose} disabled={loading}>
            {t('common.cancel')}
          </Button>
          <Button className="rounded-lg" onClick={submit} disabled={loading || !newName}>
            {loading ? <Loader2 className="h-4 w-4 animate-spin" /> : t('common.confirm')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/* ------------------------------- Mkdir Dialog ----------------------------- */

function MkdirDialog({
  open,
  basePath,
  onClose,
  onDone,
}: {
  open: boolean;
  basePath: string;
  onClose: () => void;
  onDone: () => void;
}) {
  const { t } = useTranslation();
  const [name, setName] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  const submit = async () => {
    if (!name) return;
    const fullPath = `${basePath}/${name}`;
    setLoading(true);
    setError('');
    try {
      await api.post('/files/mkdir', { path: fullPath });
      onDone();
      onClose();
      setName('');
    } catch (err) {
      setError(err instanceof ApiError ? err.message : t('files.createFailed'));
    } finally {
      setLoading(false);
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        if (!o) {
          onClose();
          setName('');
          setError('');
        }
      }}
    >
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle>{t('files.newFolderTitle')}</DialogTitle>
        </DialogHeader>
        <div className="space-y-2">
          <Label htmlFor="mkdir-input">{t('files.folderName')}</Label>
          <Input
            id="mkdir-input"
            value={name}
            onChange={(e) => setName(e.target.value)}
            autoFocus
            onKeyDown={(e) => e.key === 'Enter' && submit()}
            placeholder={t('files.folderExample')}
          />
          <p className="text-xs text-muted-foreground">
            {t('files.willCreateAt')} {basePath}/
          </p>
          {error && <p className="text-sm text-destructive">{error}</p>}
        </div>
        <DialogFooter>
          <Button variant="outline" className="rounded-lg" onClick={onClose} disabled={loading}>
            {t('common.cancel')}
          </Button>
          <Button className="rounded-lg" onClick={submit} disabled={loading || !name}>
            {loading ? <Loader2 className="h-4 w-4 animate-spin" /> : t('files.create')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/* ----------------------------- Preview Dialog ----------------------------- */

const EDITABLE_EXTENSIONS = new Set([
  '.log', '.txt', '.md', '.json', '.xml', '.yaml', '.yml', '.toml',
  '.cfg', '.conf', '.ini', '.sh', '.bash', '.zsh', '.fish',
  '.py', '.js', '.ts', '.jsx', '.tsx', '.go', '.rs', '.c', '.cpp',
  '.h', '.hpp', '.java', '.rb', '.pl', '.lua', '.vim', '.css',
  '.scss', '.less', '.html', '.htm', '.svg', '.env', '.gitignore',
  '.dockerignore', '.editorconfig', '.properties', '.sql', '.csv',
  '.tsv', '.r', '.m', '.gradle', '.cmake', '.makefile',
]);

/** Map file extension to highlight.js language identifier */
const EXT_TO_LANG: Record<string, string> = {
  '.js': 'javascript', '.jsx': 'javascript', '.mjs': 'javascript',
  '.ts': 'typescript', '.tsx': 'typescript',
  '.py': 'python', '.pyw': 'python',
  '.go': 'go',
  '.rs': 'rust',
  '.c': 'c', '.h': 'c',
  '.cpp': 'cpp', '.hpp': 'cpp', '.cc': 'cpp',
  '.java': 'java',
  '.rb': 'ruby',
  '.pl': 'perl',
  '.lua': 'lua',
  '.vim': 'vim',
  '.sh': 'bash', '.bash': 'bash', '.zsh': 'bash',
  '.fish': 'shell',
  '.css': 'css', '.scss': 'scss', '.less': 'less',
  '.html': 'xml', '.htm': 'xml', '.svg': 'xml',
  '.json': 'json',
  '.xml': 'xml',
  '.yaml': 'yaml', '.yml': 'yaml',
  '.toml': 'ini',
  '.ini': 'ini', '.cfg': 'ini', '.conf': 'ini',
  '.md': 'markdown',
  '.sql': 'sql',
  '.csv': 'plaintext',
  '.properties': 'properties',
  '.dockerfile': 'dockerfile',
  '.gradle': 'groovy',
  '.cmake': 'cmake',
  '.r': 'r',
  '.m': 'objectivec',
};

/** Derive highlight.js language from filename */
function langFromFilename(name: string): string | undefined {
  const lower = name.toLowerCase();
  // Special filenames
  if (lower === 'dockerfile' || lower === 'makefile') return lower;
  if (lower === 'cmakelists.txt') return 'cmake';
  // Extension-based
  const dotIdx = lower.lastIndexOf('.');
  if (dotIdx >= 0) {
    const ext = lower.substring(dotIdx);
    if (EXT_TO_LANG[ext]) return EXT_TO_LANG[ext];
  }
  // Dotfiles like .bashrc, .profile
  if (/^\.[a-z]/i.test(lower)) return 'bash';
  return undefined;
}

function isEditableFile(name: string): boolean {
  const lower = name.toLowerCase();
  for (const ext of EDITABLE_EXTENSIONS) {
    if (lower.endsWith(ext)) return true;
  }
  // Also match dotfiles without extensions (e.g. ".bashrc", ".profile")
  if (/^\.[a-z]/i.test(lower) && !lower.includes('.', 1)) return true;
  return false;
}

function PreviewDialog({
  target,
  onClose,
}: {
  target: FileEntry | null;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [textContent, setTextContent] = useState<string | null>(null);
  const [imgUrl, setImgUrl] = useState<string | null>(null);
  const [truncated, setTruncated] = useState(false);
  const [editing, setEditing] = useState(false);
  const [editContent, setEditContent] = useState('');
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState('');
  const [saveSuccess, setSaveSuccess] = useState(false);
  const [confirmDiscard, setConfirmDiscard] = useState<'close' | 'cancel' | null>(null);

  // Check if there are unsaved edits (editContent differs from loaded content)
  const hasUnsavedChanges = editing && editContent !== (textContent ?? '');

  const targetPath = target?.path ?? '';
  const targetName = target?.name ?? '';
  const canEdit = target !== null && !target.isDir && isEditableFile(target.name);

  useEffect(() => {
    if (!target) return;
    setLoading(true);
    setError('');
    setTextContent(null);
    setImgUrl(null);
    setTruncated(false);
    setEditing(false);
    setEditContent('');
    setSaveError('');
    setSaveSuccess(false);

    const controller = new AbortController();
    let blobUrl: string | null = null;

    api.preview(targetPath)
      .then(async (res) => {
        const ct = res.headers.get('content-type') ?? '';
        const trunc = res.headers.get('x-preview-truncated') === '1';
        setTruncated(trunc);

        if (ct.startsWith('image/')) {
          const blob = await res.blob();
          if (controller.signal.aborted) return;
          const url = URL.createObjectURL(blob);
          blobUrl = url;
          setImgUrl(url);
        } else if (
          ct.startsWith('text/') ||
          ct.includes('json') ||
          ct.includes('javascript') ||
          ct.includes('xml') ||
          ct.includes('yaml') ||
          ct.includes('shell') ||
          ct.includes('csv') ||
          ct.includes('application/x-sh')
        ) {
          const text = await res.text();
          if (controller.signal.aborted) return;
          setTextContent(text);
        } else {
          setError(t('files.cannotPreview'));
        }
      })
      .catch((err) => {
        if (controller.signal.aborted) return;
        setError(err instanceof ApiError ? err.message : t('files.previewFailed'));
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });

    return () => {
      controller.abort();
      if (blobUrl) URL.revokeObjectURL(blobUrl);
    };
  }, [target, targetPath, t]);

  const handleClose = () => {
    if (hasUnsavedChanges) {
      setConfirmDiscard('close');
      return;
    }
    if (imgUrl) URL.revokeObjectURL(imgUrl);
    onClose();
  };

  const startEditing = () => {
    setEditContent(textContent ?? '');
    setEditing(true);
    setSaveError('');
    setSaveSuccess(false);
  };

  const cancelEditing = () => {
    if (hasUnsavedChanges) {
      setConfirmDiscard('cancel');
      return;
    }
    setEditing(false);
    setEditContent('');
    setSaveError('');
    setSaveSuccess(false);
  };

  const saveEdits = async () => {
    if (!target) return;
    setSaving(true);
    setSaveError('');
    setSaveSuccess(false);
    try {
      await api.saveFileContent(targetPath, editContent);
      setTextContent(editContent);
      setEditing(false);
      setSaveSuccess(true);
      setTimeout(() => setSaveSuccess(false), 3000);
    } catch (err) {
      setSaveError(err instanceof ApiError ? err.message : t('files.saveFailed'));
    } finally {
      setSaving(false);
    }
  };

  // Syntax-highlighted HTML for preview mode
  const highlightedHtml = useMemo(() => {
    if (textContent === null) return '';
    const lang = langFromFilename(targetName);
    try {
      if (lang && hljs.getLanguage(lang)) {
        return hljs.highlight(textContent, { language: lang }).value;
      }
      return hljs.highlightAuto(textContent).value;
    } catch {
      // Fallback: escape manually
      return textContent.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
    }
  }, [textContent, targetName]);

  // Syntax-highlighted HTML for edit mode (overlay backdrop)
  const editHighlightedHtml = useMemo(() => {
    if (!editContent) return '';
    const lang = langFromFilename(targetName);
    try {
      if (lang && hljs.getLanguage(lang)) {
        return hljs.highlight(editContent, { language: lang }).value;
      }
      return hljs.highlightAuto(editContent).value;
    } catch {
      return editContent.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
    }
  }, [editContent, targetName]);

  // Shared style constants for overlay alignment (pre + textarea must match exactly)
  const codeStyle: React.CSSProperties = {
    fontFamily: 'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, "Liberation Mono", monospace',
    fontSize: '0.75rem',   // text-xs
    lineHeight: '1.625',  // leading-relaxed
    padding: '0.75rem',   // p-3
    whiteSpace: 'pre-wrap',
    wordBreak: 'break-all',
    tabSize: 4,
    letterSpacing: 'normal',
  };

  return (
    <Dialog
      open={!!target}
      onOpenChange={(o) => {
        if (!o) handleClose();
      }}
    >
      <DialogContent className="max-h-[80vh] max-w-3xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <FileIcon className="h-4 w-4" />
            <span className="truncate">{targetName}</span>
          </DialogTitle>
        </DialogHeader>
        <div className="min-h-[200px]">
          {loading && (
            <div className="flex h-40 items-center justify-center gap-2 text-sm text-muted-foreground">
              <Loader2 className="h-4 w-4 animate-spin" /> {t('files.loadingPreview')}
            </div>
          )}
          {error && !loading && (
            <div className="flex h-40 items-center justify-center text-sm text-muted-foreground">
              {error}
            </div>
          )}
          {imgUrl && !loading && (
            <div className="flex max-h-[60vh] items-center justify-center overflow-auto">
              <img src={imgUrl} alt={targetName} className="max-w-full" />
            </div>
          )}
          {textContent !== null && !loading && !editing && (
            <div>
              {truncated && (
                <p className="mb-2 text-xs text-warning">
                  {t('files.fileTooLarge')}
                </p>
              )}
              <pre className="max-h-[55vh] overflow-auto overflow-x-auto rounded-xl bg-muted/40 p-3" style={codeStyle}>
                <code
                  className="hljs"
                  dangerouslySetInnerHTML={{ __html: highlightedHtml }}
                />
              </pre>
            </div>
          )}
          {editing && (
            <div className="relative" style={{ height: '55vh' }}>
              {/* Highlighted backdrop layer */}
              <pre
                className="absolute inset-0 overflow-auto rounded-xl bg-muted/40 pointer-events-none"
                style={codeStyle}
                aria-hidden="true"
              >
                <code
                  className="hljs"
                  dangerouslySetInnerHTML={{ __html: editHighlightedHtml }}
                />
              </pre>
              {/* Transparent textarea on top */}
              <textarea
                className="absolute inset-0 w-full h-full resize-none rounded-xl bg-transparent p-3 font-mono-data text-xs leading-relaxed break-all focus:outline-none focus:ring-2 focus:ring-primary"
                style={{
                  ...codeStyle,
                  color: 'transparent',
                  caretColor: 'currentColor',
                  backgroundColor: 'transparent',
                  borderColor: 'transparent',
                }}
                value={editContent}
                onChange={(e) => setEditContent(e.target.value)}
                spellCheck={false}
                autoFocus
                wrap="soft"
              />
              {saveError && (
                <p className="mt-1 text-sm text-destructive">{saveError}</p>
              )}
            </div>
          )}
          {saveSuccess && (
            <div className="flex items-center gap-2 rounded-xl border border-green-500/40 bg-green-500/10 p-2 text-sm text-ind-emerald">
              <Save className="h-3.5 w-3.5" />
              {t('files.fileSaved')}
            </div>
          )}
        </div>
        <DialogFooter className="gap-2">
          {canEdit && !editing && textContent !== null && !truncated && (
            <Button variant="outline" className="rounded-lg" onClick={startEditing}>
              <Pencil className="h-3.5 w-3.5" /> {t('files.edit')}
            </Button>
          )}
          {editing && (
            <>
              <Button variant="outline" className="rounded-lg" onClick={cancelEditing} disabled={saving}>
                {t('common.cancel')}
              </Button>
              <Button className="rounded-lg" onClick={saveEdits} disabled={saving}>
                {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-3.5 w-3.5" />}
                {t('common.save')}
              </Button>
            </>
          )}
          <Button variant="outline" className="rounded-lg" onClick={handleClose}>
            {t('common.close')}
          </Button>
        </DialogFooter>
      </DialogContent>
      <ConfirmDialog
        open={confirmDiscard !== null}
        title={t('files.unsavedTitle')}
        description={t('files.unsavedDesc')}
        confirmText={t('files.discardChanges')}
        variant="destructive"
        onConfirm={() => {
          setEditing(false);
          setEditContent('');
          setSaveError('');
          setSaveSuccess(false);
          if (confirmDiscard === 'close') {
            if (imgUrl) URL.revokeObjectURL(imgUrl);
            onClose();
          }
          setConfirmDiscard(null);
        }}
        onCancel={() => setConfirmDiscard(null)}
      />
    </Dialog>
  );
}
