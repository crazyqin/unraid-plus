package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// smartRefreshReq is the optional body for POST /api/smart/refresh.
// All fields are optional; an empty body invalidates the entire cache.
type smartRefreshReq struct {
	// Devices restricts invalidation to the given base device names
	// (e.g. "sda", "nvme0n1"). Empty/missing → invalidate everything.
	// Unknown keys are silently skipped — see invalidateSmartCache.
	//
	// The frontend's "刷新 SMART" button sends no body (invalidates all)
	// because its purpose is to force a fresh probe of every disk after
	// the user has, say, installed smartctl or hot-swapped a drive.
	Devices []string `json:"devices,omitempty"`
}

// smartRefreshResp reports which cache entries were actually dropped.
type smartRefreshResp struct {
	Ok      bool     `json:"ok"`
	Cleared []string `json:"cleared"`
	Count   int      `json:"count"`
	Message string   `json:"message,omitempty"`
}

// SmartRefresh handles POST /api/smart/refresh.
//
// This endpoint intentionally does NOT require an active SSH connection:
// invalidation is a pure in-memory operation on the server, so calling it
// while disconnected is harmless and useful (lets the user clear stale
// cache before reconnecting). The endpoint also does NOT proactively
// re-probe — that requires a connection and a disk list, both of which the
// frontend already has via the subsequent GET /api/storage call. Keeping
// concerns split means refresh works even if smartctl is broken right now
// (cache will repopulate with "unknown" on next poll, which is the truth).
//
// Idempotent: refreshing a device that was never cached is a no-op and
// returns Count=0 with 200 OK. The frontend treats Count as informational,
// not as success/failure.
func (h *Handler) SmartRefresh(c *gin.Context) {
	var req smartRefreshReq
	// Body is optional; an empty body is the common case (refresh all).
	// We tolerate invalid JSON by falling through to "invalidate all".
	if c.Request.ContentLength > 0 {
		body, _ := io.ReadAll(c.Request.Body)
		_ = json.Unmarshal(body, &req) // best-effort
	}

	// Sanitize: strip whitespace, drop empties, deduplicate, reject
	// shell-metacharacter garbage. Though these keys are *never* passed
	// to a shell (they're only map keys), defence in depth is cheap and
	// prevents log confusion / UI weirdness if a caller sends junk.
	req.Devices = sanitizeDeviceList(req.Devices)

	cleared := invalidateSmartCache(req.Devices)

	c.JSON(http.StatusOK, smartRefreshResp{
		Ok:      true,
		Cleared: cleared,
		Count:   len(cleared),
		Message: smartRefreshMessage(len(cleared), len(req.Devices)),
	})
}

// sanitizeDeviceList trims, filters empties, and dedupes a device-name slice.
func sanitizeDeviceList(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		s := strings.TrimSpace(raw)
		if s == "" || !isValidDevName(s) {
			continue
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// isValidDevName reports whether s looks like a base device name we'd have
// cached (sd[a-z]+, nvme\d+n\d+, vd[a-z], mmcblk\d+, md\d+). Conservative —
// if a caller sends a wild-but-valid name we'd rather skip it than risk
// weirdness, since a "dropped" key for a non-existent disk has no effect
// anyway. The allowed set is just [a-zA-Z0-9] which covers every shape
// baseDevName can produce.
func isValidDevName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		isLower := r >= 'a' && r <= 'z'
		isUpper := r >= 'A' && r <= 'Z'
		isDigit := r >= '0' && r <= '9'
		if !(isLower || isUpper || isDigit) {
			return false
		}
	}
	return true
}

// smartRefreshMessage produces a short Chinese status string for the toast.
//   cleared=0, any requested → "无可刷新条目（缓存本就为空）"
//   cleared=N, requested=0   → "已刷新全部 N 块"
//   cleared=N, requested>0   → "已刷新指定 N 块"
func smartRefreshMessage(cleared, requested int) string {
	switch {
	case cleared == 0:
		return "无可刷新条目（缓存本就为空）"
	case requested == 0:
		return "已刷新全部 " + strconv.Itoa(cleared) + " 块"
	default:
		return "已刷新指定 " + strconv.Itoa(cleared) + " 块"
	}
}
