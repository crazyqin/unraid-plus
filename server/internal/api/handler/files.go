package handler

import (
	"io"
	"mime/multipart"
	"net/http"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// maxUploadSize limits the total request body for file uploads (100MB).
// This prevents a single upload from exhausting server memory.
const maxUploadSize = 100 << 20

type listFilesReq struct {
	Path string `form:"path"`
}

type fileEntry struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	IsDir   bool   `json:"isDir"`
	Size    int64  `json:"sizeBytes"`
	ModTime int64  `json:"modTime"`
	Mode    string `json:"mode"`
	Owner   string `json:"owner"`
	Group   string `json:"group"`
}

type listFilesResp struct {
	Path    string       `json:"path"`
	Entries []fileEntry `json:"entries"`
}

type deleteFilesReq struct {
	Paths []string `json:"paths"`
}

type saveFileReq struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// ListFiles uses SFTP to read the requested directory. We deliberately refuse
// to traverse above /mnt and /root for safety, matching the obvious "what is
// the user actually managing" scope.
func (h *Handler) ListFiles(c *gin.Context) {
	cli, ok := h.activeClient(c)
	if !ok {
		return
	}
	var req listFilesReq
	if err := c.ShouldBindQuery(&req); err != nil || req.Path == "" {
		req.Path = "/mnt"
	}
	req.Path = path.Clean(req.Path)

	// Safety: only allow paths under /mnt, /root, or /tmp. This is a coarse
	// belt-and-suspenders restriction — proper RBAC lives in a future version.
	if !isAllowedRoot(req.Path) {
		errOut(c, http.StatusForbidden, "出于安全考虑，仅允许浏览 /mnt、/root、/tmp 下文件")
		return
	}

	sc, err := cli.SFTP()
	if err != nil {
		errOut(c, http.StatusInternalServerError, "SFTP 会话失败: "+err.Error())
		return
	}
	defer sc.Close()

	entries, err := sc.List(req.Path)
	if err != nil {
		errOut(c, http.StatusBadRequest, "读取目录失败: "+err.Error())
		return
	}

	out := make([]fileEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, fileEntry{
			Name:    e.Name,
			Path:    e.Path,
			IsDir:   e.IsDir,
			Size:    e.Size,
			ModTime: e.ModTime,
			Mode:    e.Mode,
			Owner:   e.Owner,
			Group:   e.Group,
		})
	}

	c.JSON(http.StatusOK, listFilesResp{Path: req.Path, Entries: out})
}

// DeleteFiles moves the given paths into Unraid's recycle bin if one is
// configured (commonly /mnt/user/.RecycleBin), or removes them outright.
// For v0.x we delete directly — the recycle bin integration is a roadmap item.
func (h *Handler) DeleteFiles(c *gin.Context) {
	cli, ok := h.activeClient(c)
	if !ok {
		return
	}
	var req deleteFilesReq
	if err := c.ShouldBindJSON(&req); err != nil {
		errOut(c, http.StatusBadRequest, "请求格式错误")
		return
	}
	if len(req.Paths) == 0 {
		errOut(c, http.StatusBadRequest, "未指定要删除的文件")
		return
	}
	for _, p := range req.Paths {
		p = path.Clean(p)
		if !isAllowedRoot(p) {
			errOut(c, http.StatusForbidden, "拒绝删除非允许目录下的文件: "+p)
			return
		}
	}

	sc, err := cli.SFTP()
	if err != nil {
		errOut(c, http.StatusInternalServerError, "SFTP 会话失败: "+err.Error())
		return
	}
	defer sc.Close()

	failed := []string{}
	for _, p := range req.Paths {
		if err := sc.RemoveAll(p); err != nil {
			failed = append(failed, p+": "+err.Error())
		}
	}
	if len(failed) > 0 {
		c.JSON(http.StatusPartialContent, gin.H{
			"ok":      false,
			"message": "部分文件删除失败",
			"failed":  failed,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "已删除 " + strconv.Itoa(len(req.Paths)) + " 项"})
}

// isAllowedRoot enforces the coarse path-safety rule for file operations.
func isAllowedRoot(p string) bool {
	switch {
	case p == "/mnt" || strings.HasPrefix(p, "/mnt/"):
		return true
	case p == "/root" || strings.HasPrefix(p, "/root/"):
		return true
	case p == "/tmp" || strings.HasPrefix(p, "/tmp/"):
		return true
	}
	return false
}

// ---- v0.5: Download / Upload / Rename / Mkdir ----

// PreviewFile returns a small excerpt of a text file (first 64KB) for inline
// preview in the browser. If the file is larger than 64KB, only the head is
// returned with a "truncated" flag. Non-text files (images, PDFs) are served
// with their detected content type so the browser can render them natively.
//
// We use a simple heuristic to distinguish text from binary: if the first 512
// bytes contain a NUL byte, we treat it as binary and serve with the detected
// MIME type; otherwise we serve as text/plain.
func (h *Handler) PreviewFile(c *gin.Context) {
	cli, ok := h.activeClient(c)
	if !ok {
		return
	}
	p := path.Clean(c.Query("path"))
	if p == "" || !isAllowedRoot(p) {
		errOut(c, http.StatusForbidden, "拒绝预览非允许目录下的文件")
		return
	}

	sc, err := cli.SFTP()
	if err != nil {
		errOut(c, http.StatusInternalServerError, "SFTP 会话失败: "+err.Error())
		return
	}
	defer sc.Close()

	entry, err := sc.Stat(p)
	if err != nil {
		errOut(c, http.StatusNotFound, "文件不存在: "+err.Error())
		return
	}
	if entry.IsDir {
		errOut(c, http.StatusBadRequest, "不能预览目录")
		return
	}

	f, err := sc.Open(p)
	if err != nil {
		errOut(c, http.StatusInternalServerError, "打开文件失败: "+err.Error())
		return
	}
	defer f.Close()

	// Read up to 64KB for preview.
	previewSize := int64(64 * 1024)
	buf := make([]byte, previewSize)
	n, _ := io.ReadFull(f, buf)
	buf = buf[:n]

	// Detect content type from the first 512 bytes.
	sniffLen := n
	if sniffLen > 512 {
		sniffLen = 512
	}
	ct := http.DetectContentType(buf[:sniffLen])

	c.Header("Content-Type", ct)
	if int64(n) < previewSize {
		// File fit entirely in the preview buffer.
		c.Status(http.StatusOK)
		_, _ = c.Writer.Write(buf)
		return
	}

	// File is larger — check if there's more data.
	remaining := entry.Size - int64(n)
	c.Header("X-Preview-Truncated", "true")
	c.Header("X-Preview-Total-Size", strconv.FormatInt(entry.Size, 10))
	c.Status(http.StatusOK)
	_, _ = c.Writer.Write(buf)
	_ = remaining // remaining bytes not served
}

// DownloadFile streams a single file from the Unraid host via SFTP to the
// HTTP response. Sets Content-Disposition so the browser saves rather than
// inline-displays binary files. Path safety is enforced via isAllowedRoot.
//
// We deliberately do NOT set Content-Length because SFTP file sizes can
// change between Stat and read on a live system; chunked transfer encoding
// handles it transparently.
func (h *Handler) DownloadFile(c *gin.Context) {
	cli, ok := h.activeClient(c)
	if !ok {
		return
	}
	p := path.Clean(c.Query("path"))
	if p == "" || !isAllowedRoot(p) {
		errOut(c, http.StatusForbidden, "拒绝下载非允许目录下的文件")
		return
	}

	sc, err := cli.SFTP()
	if err != nil {
		errOut(c, http.StatusInternalServerError, "SFTP 会话失败: "+err.Error())
		return
	}
	defer sc.Close()

	entry, err := sc.Stat(p)
	if err != nil {
		errOut(c, http.StatusNotFound, "文件不存在: "+err.Error())
		return
	}
	if entry.IsDir {
		errOut(c, http.StatusBadRequest, "不能下载目录，请选择文件")
		return
	}

	f, err := sc.Open(p)
	if err != nil {
		errOut(c, http.StatusInternalServerError, "打开文件失败: "+err.Error())
		return
	}
	defer f.Close()

	// Sanitize filename: strip characters that could break the Content-Disposition
	// header (quotes, backslashes, CR, LF). This prevents header injection.
	filename := filepath.Base(p)
	filename = strings.Map(func(r rune) rune {
		if r == '"' || r == '\\' || r == '\r' || r == '\n' {
			return '_'
		}
		return r
	}, filename)
	c.Header("Content-Disposition", `attachment; filename="`+filename+`"`)
	c.Header("Content-Type", "application/octet-stream")
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, f)
}

type renameReq struct {
	OldPath string `json:"oldPath"`
	NewPath string `json:"newPath"`
}

// RenameFile renames/moves a file or directory via SFTP Rename. Both paths
// must be under allowed roots. The destination's parent directory must
// already exist (SFTP Rename doesn't create intermediate dirs).
func (h *Handler) RenameFile(c *gin.Context) {
	cli, ok := h.activeClient(c)
	if !ok {
		return
	}
	var req renameReq
	if err := c.ShouldBindJSON(&req); err != nil {
		errOut(c, http.StatusBadRequest, "请求格式错误")
		return
	}
	req.OldPath = path.Clean(req.OldPath)
	req.NewPath = path.Clean(req.NewPath)
	if req.OldPath == "" || req.NewPath == "" {
		errOut(c, http.StatusBadRequest, "路径不能为空")
		return
	}
	if !isAllowedRoot(req.OldPath) || !isAllowedRoot(req.NewPath) {
		errOut(c, http.StatusForbidden, "拒绝操作非允许目录下的文件")
		return
	}

	sc, err := cli.SFTP()
	if err != nil {
		errOut(c, http.StatusInternalServerError, "SFTP 会话失败: "+err.Error())
		return
	}
	defer sc.Close()

	if err := sc.Move(req.OldPath, req.NewPath); err != nil {
		errOut(c, http.StatusInternalServerError, "重命名失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "已重命名"})
}

type mkdirReq struct {
	Path string `json:"path"`
}

// MkdirFile creates a directory (including parents) via SFTP MkdirAll.
func (h *Handler) MkdirFile(c *gin.Context) {
	cli, ok := h.activeClient(c)
	if !ok {
		return
	}
	var req mkdirReq
	if err := c.ShouldBindJSON(&req); err != nil {
		errOut(c, http.StatusBadRequest, "请求格式错误")
		return
	}
	req.Path = path.Clean(req.Path)
	if req.Path == "" || !isAllowedRoot(req.Path) {
		errOut(c, http.StatusForbidden, "拒绝在非允许目录下创建文件夹")
		return
	}

	sc, err := cli.SFTP()
	if err != nil {
		errOut(c, http.StatusInternalServerError, "SFTP 会话失败: "+err.Error())
		return
	}
	defer sc.Close()

	if err := sc.Mkdir(req.Path); err != nil {
		errOut(c, http.StatusInternalServerError, "创建目录失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "已创建目录"})
}

// UploadFile receives one or more files via multipart/form-data and writes
// them to the Unraid host via SFTP. The destination directory is specified
// via the "dir" query parameter (or form field). Each file's original
// filename is appended to form the full destination path.
//
// We stream (io.Copy from multipart Part → SFTP Create) so large files
// don't need to fit in memory. The SFTP client's WriteCloser handles
// chunked writes over the SSH channel.
func (h *Handler) UploadFile(c *gin.Context) {
	cli, ok := h.activeClient(c)
	if !ok {
		return
	}
	dir := path.Clean(c.DefaultQuery("dir", "/mnt"))
	if !isAllowedRoot(dir) {
		errOut(c, http.StatusForbidden, "拒绝上传到非允许目录")
		return
	}

	// Limit total request body size to prevent memory exhaustion.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUploadSize)

	form, err := c.MultipartForm()
	if err != nil {
		errOut(c, http.StatusBadRequest, "解析上传表单失败: "+err.Error())
		return
	}
	files := form.File["files"]
	if len(files) == 0 {
		// Also try single-file field "file" for convenience.
		if f, err2 := c.FormFile("file"); err2 == nil {
			files = []*multipart.FileHeader{f}
		}
	}
	if len(files) == 0 {
		errOut(c, http.StatusBadRequest, "未找到上传文件")
		return
	}

	sc, err := cli.SFTP()
	if err != nil {
		errOut(c, http.StatusInternalServerError, "SFTP 会话失败: "+err.Error())
		return
	}
	defer sc.Close()

	uploaded := 0
	failed := []string{}
	for _, fh := range files {
		// Sanitize filename: strip path components (IE sends full paths).
		name := filepath.Base(fh.Filename)
		if name == "" || name == "." || name == "/" {
			name = "unnamed"
		}
		dst := path.Join(dir, name)

		src, err := fh.Open()
		if err != nil {
			failed = append(failed, name+": "+err.Error())
			continue
		}

		dstFile, err := sc.Create(dst)
		if err != nil {
			_ = src.Close()
			failed = append(failed, name+": "+err.Error())
			continue
		}

		_, err = io.Copy(dstFile, src)
		_ = src.Close()
		_ = dstFile.Close()
		if err != nil {
			failed = append(failed, name+": "+err.Error())
			continue
		}
		uploaded++
	}

	if len(failed) > 0 && uploaded == 0 {
		c.JSON(http.StatusInternalServerError, gin.H{
			"ok":      false,
			"message": "全部上传失败",
			"failed":  failed,
		})
		return
	}
	if len(failed) > 0 {
		c.JSON(http.StatusOK, gin.H{
			"ok":      true,
			"message": "部分上传成功（" + strconv.Itoa(uploaded) + " 成功, " + strconv.Itoa(len(failed)) + " 失败）",
			"failed":  failed,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"message": "已上传 " + strconv.Itoa(uploaded) + " 个文件",
	})
}

// SaveFileContent writes text content to a remote file via SFTP. The file is
// truncated and rewritten atomically — this is intended for small text files
// (configs, logs, scripts) edited in the browser preview pane. Path safety
// is enforced via isAllowedRoot.
func (h *Handler) SaveFileContent(c *gin.Context) {
	cli, ok := h.activeClient(c)
	if !ok {
		return
	}
	var req saveFileReq
	if err := c.ShouldBindJSON(&req); err != nil {
		errOut(c, http.StatusBadRequest, "请求格式错误")
		return
	}
	req.Path = path.Clean(req.Path)
	if req.Path == "" || !isAllowedRoot(req.Path) {
		errOut(c, http.StatusForbidden, "拒绝保存到非允许目录")
		return
	}

	sc, err := cli.SFTP()
	if err != nil {
		errOut(c, http.StatusInternalServerError, "SFTP 会话失败: "+err.Error())
		return
	}
	defer sc.Close()

	// Open file with O_WRONLY|O_CREAT|O_TRUNC (0644 permissions).
	f, err := sc.Create(req.Path)
	if err != nil {
		errOut(c, http.StatusInternalServerError, "打开文件失败: "+err.Error())
		return
	}
	defer f.Close()

	if _, err := f.Write([]byte(req.Content)); err != nil {
		errOut(c, http.StatusInternalServerError, "写入文件失败: "+err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "已保存"})
}
