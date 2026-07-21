import { useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import {
  ChevronRight,
  Download,
  File as FileIcon,
  Folder,
  Home,
  Loader2,
  Trash2,
  Upload,
} from 'lucide-react';
import { api } from '@/lib/api';
import { Button } from '@/components/ui/button';
import { Card, CardContent } from '@/components/ui/card';
import { formatBytes, timeAgo, cn } from '@/lib/utils';
import type { FileEntry, ListFilesResponse } from '@/types';

export default function FilesPage() {
  const [path, setPath] = useState('/mnt/user');
  const [selected, setSelected] = useState<Set<string>>(new Set());
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
    if (!confirm(`确认删除 ${selected.size} 个文件？删除后可在回收站找回。`)) return;
    await api.post('/files/delete', { paths: [...selected] });
    setSelected(new Set());
    qc.invalidateQueries({ queryKey: ['files', path] });
  };

  return (
    <div className="flex h-full flex-col p-4 md:p-6">
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
          <Button size="sm" variant="outline" disabled>
            <Upload className="h-3.5 w-3.5" /> 上传
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
            disabled={selected.size === 0}
            onClick={() => alert('下载能力在后端 SFTP 完成后激活')}
          >
            <Download className="h-3.5 w-3.5" /> 下载
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
    </div>
  );
}
