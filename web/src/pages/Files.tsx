import { useEffect, useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import {
  ChevronRight,
  Download,
  Eye,
  File as FileIcon,
  Folder,
  FolderPlus,
  Home,
  Loader2,
  Pencil,
  Save,
  Trash2,
  Upload,
} from 'lucide-react';
import { api, ApiError } from '@/lib/api';
import { Button } from '@/components/ui/button';
import { Card, CardContent } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Progress } from '@/components/ui/progress';
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

export default function FilesPage() {
  const [path, setPath] = useState('/mnt/user');
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [uploadProgress, setUploadProgress] = useState<number | null>(null);
  const [dragOver, setDragOver] = useState(false);
  const [renameTarget, setRenameTarget] = useState<FileEntry | null>(null);
  const [mkdirOpen, setMkdirOpen] = useState(false);
  const [previewTarget, setPreviewTarget] = useState<FileEntry | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [uploadError, setUploadError] = useState<string | null>(null);
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
    try {
      await api.post('/files/delete', { paths: [...selected] });
      setSelected(new Set());
      qc.invalidateQueries({ queryKey: ['files', path] });
    } catch (err) {
      setUploadError(err instanceof ApiError ? err.message : '删除失败');
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
      setUploadError(err instanceof ApiError ? err.message : '上传失败');
    } finally {
      setUploadProgress(null);
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
            disabled={uploadProgress !== null}
            onClick={() => fileInputRef.current?.click()}
          >
            {uploadProgress !== null ? (
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
            onClick={() => setConfirmDelete(true)}
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
          <Button
            size="sm"
            variant="outline"
            disabled={selected.size !== 1 || selectedEntries[0]?.isDir}
            onClick={() => selectedEntries[0] && setPreviewTarget(selectedEntries[0])}
          >
            <Eye className="h-3.5 w-3.5" /> 预览
          </Button>
        </div>
      </div>

      {/* Upload progress bar */}
      {uploadProgress !== null && (
        <div className="mb-2 flex items-center gap-2 rounded-md border bg-muted/30 p-2">
          <span className="text-xs text-muted-foreground">上传中…</span>
          <Progress className="flex-1" value={uploadProgress} />
          <span className="text-xs tabular-nums text-muted-foreground">{uploadProgress}%</span>
        </div>
      )}

      {/* Upload error toast */}
      {uploadError && (
        <div className="mb-2 flex items-center justify-between rounded-md border border-destructive/40 bg-destructive/10 p-2 text-sm text-destructive">
          <span>{uploadError}</span>
          <button
            className="text-xs underline"
            onClick={() => setUploadError(null)}
          >
            关闭
          </button>
        </div>
      )}

      <Card
        className="relative flex-1 overflow-hidden"
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
                      onDoubleClick={() => !e.isDir && setPreviewTarget(e)}
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

        {/* Drag-and-drop overlay */}
        {dragOver && (
          <div className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center rounded-lg border-2 border-dashed border-primary bg-primary/10 backdrop-blur-sm">
            <div className="flex flex-col items-center gap-2 text-primary">
              <Upload className="h-8 w-8" />
              <span className="text-sm font-medium">松开以上传文件到当前目录</span>
            </div>
          </div>
        )}
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
      <PreviewDialog
        target={previewTarget}
        onClose={() => setPreviewTarget(null)}
      />
      <ConfirmDialog
        open={confirmDelete}
        title="确认删除"
        description={`确认删除 ${selected.size} 个文件？此操作不可恢复。`}
        confirmText="删除"
        variant="destructive"
        onConfirm={del}
        onCancel={() => setConfirmDelete(false)}
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
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [textContent, setTextContent] = useState<string | null>(null);
  const [imgUrl, setImgUrl] = useState<string | null>(null);
  const [truncated, setTruncated] = useState(false);
  const [editing, setEditing] = useState(false);
  const [editContent, setEditContent] = useState('');
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState('');

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

    let revokeUrl: string | null = null;

    api.preview(targetPath)
      .then(async (res) => {
        const ct = res.headers.get('content-type') ?? '';
        const trunc = res.headers.get('x-preview-truncated') === '1';
        setTruncated(trunc);

        if (ct.startsWith('image/')) {
          const blob = await res.blob();
          const url = URL.createObjectURL(blob);
          revokeUrl = url;
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
          setTextContent(text);
        } else {
          setError('不支持预览此文件类型，请下载后查看。');
        }
      })
      .catch((err) => {
        setError(err instanceof ApiError ? err.message : '预览加载失败');
      })
      .finally(() => setLoading(false));

    return () => {
      if (revokeUrl) URL.revokeObjectURL(revokeUrl);
    };
  }, [target, targetPath]);

  const handleClose = () => {
    if (imgUrl) URL.revokeObjectURL(imgUrl);
    onClose();
  };

  const startEditing = () => {
    setEditContent(textContent ?? '');
    setEditing(true);
    setSaveError('');
  };

  const cancelEditing = () => {
    setEditing(false);
    setEditContent('');
    setSaveError('');
  };

  const saveEdits = async () => {
    if (!target) return;
    setSaving(true);
    setSaveError('');
    try {
      await api.saveFileContent(targetPath, editContent);
      setTextContent(editContent);
      setEditing(false);
    } catch (err) {
      setSaveError(err instanceof ApiError ? err.message : '保存失败');
    } finally {
      setSaving(false);
    }
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
              <Loader2 className="h-4 w-4 animate-spin" /> 加载预览…
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
                  文件较大，仅显示前 64KB 内容。
                </p>
              )}
              <pre className="max-h-[55vh] overflow-auto overflow-x-auto whitespace-pre-wrap break-all rounded-md bg-muted/40 p-3 text-xs leading-relaxed">
                <code>{textContent}</code>
              </pre>
            </div>
          )}
          {editing && (
            <div>
              <textarea
                className="h-[55vh] w-full resize-none rounded-md border bg-muted/40 p-3 font-mono text-xs leading-relaxed break-all focus:outline-none focus:ring-2 focus:ring-primary"
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
        </div>
        <DialogFooter className="gap-2">
          {canEdit && !editing && textContent !== null && !truncated && (
            <Button variant="outline" onClick={startEditing}>
              <Pencil className="h-3.5 w-3.5" /> 编辑
            </Button>
          )}
          {editing && (
            <>
              <Button variant="outline" onClick={cancelEditing} disabled={saving}>
                取消
              </Button>
              <Button onClick={saveEdits} disabled={saving}>
                {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-3.5 w-3.5" />}
                保存
              </Button>
            </>
          )}
          <Button variant="outline" onClick={handleClose}>
            关闭
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
