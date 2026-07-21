package handler

import (
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

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
		req.Path = "/mnt/user"
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
