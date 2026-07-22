import { useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import {
  ChevronRight,
  Download,
  File as FileIcon,
  Folder,
  FolderPlus,
  Home,
  Loader2,
  Pencil,
  Trash2,
  Upload,
} from 'lucide-react';
import { api, ApiError } from '@/lib/api';
import { Button } from '@/components/ui/button';
import { Card, CardContent } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { formatBytes, timeAgo, cn } from '@/lib/utils';
import type { FileEntry, ListFilesResponse } from '@/types';

export default function FilesPage() {
  const [path, setPath] = useState('/mnt/user');
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [uploading, setUploading] = useState(false);
  const [renameTarget, setRenameTarget] = useState<FileEntry | null>(null);
  const [mkdirOpen, setMkdirOpen] = useState(false);
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
    setPath(next || '/');
  };

  const toggle = (p: string) =>
    setSelected((s) => {
      const n = new Set(s);
      n.has(p) ? n.delete(p) : n.add(p);
      return n;
    });

  const del = async () => {
    if (selected.size === 0) return;
    if (!confirm(`确认删除 ${selected.size} 个文件？此操作不可恢复。`)) return;
    await api.post('/files/delete', { paths: [...selected] });
    setSelected(new Set());
    qc.invalidateQueries({ queryKey: ['files', path] });
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
    setUploading(true);
    try {
      const formData = new FormData();
      for (const f of files) {
        formData.append('files', f);
      }
      await api.upload(`/files/upload?dir=${encodeURIComponent(path)}`, formData);
      qc.invalidateQueries({ queryKey: ['files', path] });
    } catch (err) {
      const msg = err instanceof ApiError ? err.message : '上传失败';
      alert(msg);
    } finally {
      setUploading(false);
      if (fileInputRef.current) fileInputRef.current.value = '';
    }
  };

  const selectedEntries = (data?.entries ?? []).filter((e) =>
    selected.has(e.path),
  );

  return (
    <div className="flex h-full flex-col p-4 md:p-6">
      {/* Hidden file input for uploads */}
      <input
        ref={fileInputRef}
        type="file"
        multiple
        className="hidden"
        onChange={(e) => e.target.files && upload(e.target.files)}
      />

      {/* Toolbar */}
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-1 text-sm">
          <button
            onClick={() => goto(-1)}
            className="flex items-center rounded px-1.5 py-1 hover:bg-accent"
          >
            <Home className="h-4 w-4" />
          </button>
          {breadcrumbs.map((seg, i) => (
            <div key={i} className="flex items-center">
              <ChevronRight className="h-3 w-3 text-muted-foreground" />
              <button
                onClick={() => goto(i)}
                className="rounded px-1.5 py-1 hover:bg-accent"
              >
                {seg}
              </button>
            </div>
          ))}
        </div>
        <div className="flex items-center gap-2">
          <Button
            size="sm"
            variant="outline"
            disabled={uploading}
            onClick={() => fileInputRef.current?.click()}
          >
            {uploading ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <Upload className="h-3.5 w-3.5" />
            )}
            上传
          </Button>
          <Button
            size="sm"
            variant="outline"
            onClick={() => setMkdirOpen(true)}
          >
            <FolderPlus className="h-3.5 w-3.5" /> 新建文件夹
          </Button>
          <Button
            size="sm"
            variant="outline"
            disabled={selected.size === 0}
            onClick={del}
          >
            <Trash2 className="h-3.5 w-3.5" /> 删除
          </Button>
          <Button
            size="sm"
            variant="outline"
            disabled={selected.size !== 1 || selectedEntries[0]?.isDir}
            onClick={download}
            title={selected.size === 1 && selectedEntries[0]?.isDir ? '不能下载目录' : ''}
          >
            <Download className="h-3.5 w-3.5" /> 下载
          </Button>
          <Button
            size="sm"
            variant="outline"
            disabled={selected.size !== 1}
            onClick={() => selectedEntries[0] && setRenameTarget(selectedEntries[0])}
          >
            <Pencil className="h-3.5 w-3.5" /> 重命名
          </Button>
        </div>
      </div>

      <Card className="flex-1 overflow-hidden">
        <CardContent className="h-full p-0">
          {isLoading ? (
            <div className="flex h-full items-center justify-center gap-2 text-sm text-muted-foreground">
              <Loader2 className="h-4 w-4 animate-spin" /> 读取目录…
            </div>
          ) : isError ? (
            <div className="flex h-full items-center justify-center text-sm text-destructive">
              无法读取目录。请确认后端 SFTP 已就绪。
            </div>
          ) : (
            <div className="h-full overflow-auto">
              <table className="w-full text-sm">
                <thead className="sticky top-0 bg-card text-xs text-muted-foreground">
                  <tr className="border-b">
                    <th className="px-3 py-2 text-left font-medium">名称</th>
                    <th className="px-3 py-2 text-right font-medium">大小</th>
                    <th className="px-3 py-2 text-right font-medium">修改时间</th>
                  </tr>
                </thead>
                <tbody>
                  {(data?.entries ?? []).map((e) => (
                    <tr
                      key={e.path}
                      onClick={() => (e.isDir ? enter(e) : toggle(e.path))}
                      onDoubleClick={() => !e.isDir && setRenameTarget(e)}
                      className={cn(
                        'cursor-pointer border-b border-border/50 hover:bg-accent/50',
                        selected.has(e.path) && 'bg-primary/10',
                      )}
                    >
                      <td className="px-3 py-2">
                        <div className="flex items-center gap-2">
                          {e.isDir ? (
                            <Folder className="h-4 w-4 text-amber-500" />
                          ) : (
                            <FileIcon className="h-4 w-4 text-muted-foreground" />
                          )}
                          <span className="truncate">{e.name}</span>
                        </div>
                      </td>
                      <td className="px-3 py-2 text-right tabular-nums text-muted-foreground">
                        {e.isDir ? '—' : formatBytes(e.sizeBytes)}
                      </td>
                      <td className="px-3 py-2 text-right tabular-nums text-muted-foreground">
                        {timeAgo(e.modTime)}
                      </td>
                    </tr>
                  ))}
                  {(data?.entries ?? []).length === 0 && (
                    <tr>
                      <td colSpan={3} className="px-3 py-8 text-center text-muted-foreground">
                        空目录
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>

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
    </div>
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
      setError(err instanceof ApiError ? err.message : '重命名失败');
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
          <DialogTitle>重命名</DialogTitle>
        </DialogHeader>
        <div className="space-y-2">
          <Label htmlFor="rename-input">新名称</Label>
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
          <Button variant="outline" onClick={onClose} disabled={loading}>
            取消
          </Button>
          <Button onClick={submit} disabled={loading || !newName}>
            {loading ? <Loader2 className="h-4 w-4 animate-spin" /> : '确认'}
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
      setError(err instanceof ApiError ? err.message : '创建失败');
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
          <DialogTitle>新建文件夹</DialogTitle>
        </DialogHeader>
        <div className="space-y-2">
          <Label htmlFor="mkdir-input">文件夹名称</Label>
          <Input
            id="mkdir-input"
            value={name}
            onChange={(e) => setName(e.target.value)}
            autoFocus
            onKeyDown={(e) => e.key === 'Enter' && submit()}
            placeholder="例如：photos"
          />
          <p className="text-xs text-muted-foreground">
            将创建于 {basePath}/
          </p>
          {error && <p className="text-sm text-destructive">{error}</p>}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose} disabled={loading}>
            取消
          </Button>
          <Button onClick={submit} disabled={loading || !name}>
            {loading ? <Loader2 className="h-4 w-4 animate-spin" /> : '创建'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
