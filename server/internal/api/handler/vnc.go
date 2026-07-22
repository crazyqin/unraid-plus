package handler

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/crazyqin/unraid-plus/server/pkg/logger"
)

// vncUpgrader is the WebSocket upgrader for VNC proxy connections.
var vncUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// VNCProxy handles a WebSocket connection that bridges to a VNC server on the
// Unraid host through an SSH tunnel. The VM ID is passed as a query parameter
// (e.g. /ws/vnc?vm=myvm). The handler:
//  1. Looks up the VNC display for the VM via `virsh vncdisplay <vm>`
//  2. Opens a TCP connection through the SSH tunnel to that VNC address
//  3. Bridges WebSocket frames ↔ raw TCP bytes (VNC is a binary protocol)
//
// The frontend uses the noVNC library which speaks the VNC protocol over
// WebSocket. noVNC expects the server to speak raw VNC — we just proxy bytes.
func (h *Handler) VNCProxy(c *gin.Context) {
	cli, ok := h.activeClient(c)
	if !ok {
		return
	}

	vmName := c.Query("vm")
	if vmName == "" {
		errOut(c, http.StatusBadRequest, "缺少 vm 参数")
		return
	}

	// Look up VNC address for this VM via virsh vncdisplay.
	// Output format: ":0" or "127.0.0.1:0" (port = 5900 + display number)
	vncOut, err := cli.Run("virsh vncdisplay " + shellQuote(vmName) + " 2>/dev/null")
	if err != nil || strings.TrimSpace(vncOut) == "" {
		errOut(c, http.StatusNotFound, "无法获取该虚拟机的 VNC 地址（可能未运行或未配置 VNC）")
		return
	}
	vncAddr := parseVNCDisplay(strings.TrimSpace(vncOut))
	if vncAddr == "" {
		errOut(c, http.StatusInternalServerError, "无法解析 VNC 地址: "+vncOut)
		return
	}

	// Open SSH-tunneled TCP connection to the VNC port.
	vncConn, err := cli.DialTCP("tcp", vncAddr)
	if err != nil {
		errOut(c, http.StatusBadGateway, "VNC 连接失败: "+err.Error())
		return
	}
	defer vncConn.Close()

	// Upgrade to WebSocket.
	ws, err := vncUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logger.Warnf("vnc ws upgrade failed: %v", err)
		return
	}
	defer ws.Close()

	logger.Infof("vnc proxy: %s -> %s", vmName, vncAddr)

	// Bridge: VNC TCP → WebSocket (read from TCP, write to WS as BinaryMessage)
	done := make(chan struct{}, 2)

	// TCP → WS
	go func() {
		defer func() { done <- struct{}{} }()
		buf := make([]byte, 32768)
		for {
			n, err := vncConn.Read(buf)
			if n > 0 {
				if werr := ws.WriteMessage(websocket.BinaryMessage, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// WS → TCP
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			mtype, data, err := ws.ReadMessage()
			if err != nil {
				return
			}
			switch mtype {
			case websocket.BinaryMessage, websocket.TextMessage:
				if _, werr := vncConn.Write(data); werr != nil {
					return
				}
			}
		}
	}()

	// Wait for either direction to finish, then close both.
	<-done

	// Give a brief moment for the other direction to flush.
	vncConn.SetDeadline(time.Now().Add(500 * time.Millisecond))
	ws.SetWriteDeadline(time.Now().Add(500 * time.Millisecond))
}

// parseVNCDisplay converts `virsh vncdisplay` output to a TCP address.
// Input:  ":0", ":1", "127.0.0.1:0", "localhost:5900"
// Output: "127.0.0.1:5900", "127.0.0.1:5901", etc.
func parseVNCDisplay(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}

	// Strip leading protocol if present (shouldn't be, but defensive)
	s = strings.TrimPrefix(s, "vnc://")

	host := "127.0.0.1"
	var port string

	if strings.Contains(s, ":") {
		parts := strings.SplitN(s, ":", 2)
		if parts[0] != "" {
			host = parts[0]
		}
		port = parts[1]
	} else {
		return ""
	}

	// If port is a display number (e.g. "0", "1"), convert to 5900+N
	if p, ok := isDisplayNumber(port); ok {
		return host + ":" + strconv.Itoa(5900+p)
	}

	// Already a port number
	return host + ":" + port
}

func isDisplayNumber(s string) (int, bool) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// VNCInfo returns the VNC address for a VM (if available). This is called
// from ListVMs to include VNC info in the VM list.
func (h *Handler) VNCInfo(vmName string) string {
	cli, err := h.pool.Active()
	if err != nil {
		return ""
	}
	out, err := cli.Run("virsh vncdisplay " + shellQuote(vmName) + " 2>/dev/null")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// VNCDialInfo is a lightweight endpoint that returns VNC connection details
// for a specific VM, so the frontend knows whether VNC is available.
func (h *Handler) VNCDialInfo(c *gin.Context) {
	cli, ok := h.activeClient(c)
	if !ok {
		return
	}
	vmName := c.Query("vm")
	if vmName == "" {
		errOut(c, http.StatusBadRequest, "缺少 vm 参数")
		return
	}

	out, err := cli.Run("virsh vncdisplay " + shellQuote(vmName) + " 2>/dev/null")
	vncAddr := ""
	available := false
	if err == nil {
		raw := strings.TrimSpace(out)
		if raw != "" {
			parsed := parseVNCDisplay(raw)
			if parsed != "" {
				vncAddr = raw
				available = true
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"available": available,
		"vncAddr":   vncAddr,
	})
}

// Ensure io is available for the VNC proxy (suppress unused import warning).
var _ = io.EOF
