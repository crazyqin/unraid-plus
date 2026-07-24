package unraid

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/crazyqin/unraid-plus/server/pkg/logger"
)

// GraphQLRequest is the standard GraphQL HTTP request body.
type GraphQLRequest struct {
	Query         string                 `json:"query"`
	Variables     map[string]interface{} `json:"variables,omitempty"`
	OperationName string                `json:"operationName,omitempty"`
}

// GraphQLResponse is the standard GraphQL HTTP response body.
type GraphQLResponse struct {
	Data   json.RawMessage   `json:"data,omitempty"`
	Errors []GraphQLError    `json:"errors,omitempty"`
}

// GraphQLError represents a single GraphQL error.
type GraphQLError struct {
	Message   string `json:"message"`
	Path      []interface{} `json:"path,omitempty"`
}

// GraphQLQuery sends a GraphQL query to the Unraid server's /graphql endpoint.
// It uses the stored session cookies for authentication (same as WebGUI session).
// Returns the raw response data and any GraphQL errors.
func (c *Client) GraphQLQuery(serverID string, query string, variables map[string]interface{}) (json.RawMessage, error) {
	return c.GraphQLQueryWithOp(serverID, query, variables, "")
}

// GraphQLQueryWithOp sends a named GraphQL operation.
func (c *Client) GraphQLQueryWithOp(serverID string, query string, variables map[string]interface{}, opName string) (json.RawMessage, error) {
	sess := c.getSession(serverID)
	if sess == nil {
		return nil, fmt.Errorf("no api session for server %s", serverID)
	}

	gqlURL := sess.apiBase + "/graphql"

	reqBody := GraphQLRequest{
		Query:     query,
		Variables: variables,
	}
	if opName != "" {
		reqBody.OperationName = opName
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal graphql request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, gqlURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	// Attach session cookies
	parsedURL, _ := url.Parse(gqlURL)
	if sess.jar != nil {
		for _, ck := range sess.jar.Cookies(parsedURL) {
			req.AddCookie(ck)
		}
	}

	// Also attach x-api-key header if configured
	if sess.apiKey != "" {
		req.Header.Set("x-api-key", sess.apiKey)
	}

	transport := makeTransport()
	httpc := &http.Client{
		Timeout:   c.timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Don't follow redirects for GraphQL - they indicate auth issues
			if len(via) >= 3 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
	defer httpc.CloseIdleConnections()

	resp, err := httpc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("graphql request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	// Update cookies from response
	if sess.jar != nil {
		sess.jar.SetCookies(parsedURL, resp.Cookies())
	}

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("graphql auth failed (HTTP %d): ensure the GraphQL API is enabled and session cookies are valid", resp.StatusCode)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("graphql request returned HTTP %d: %s", resp.StatusCode, truncateString(string(body), 200))
	}

	var gqlResp GraphQLResponse
	if err := json.Unmarshal(body, &gqlResp); err != nil {
		return nil, fmt.Errorf("parse graphql response: %w (body: %s)", err, truncateString(string(body), 200))
	}

	if len(gqlResp.Errors) > 0 {
		msgs := make([]string, 0, len(gqlResp.Errors))
		for _, e := range gqlResp.Errors {
			msgs = append(msgs, e.Message)
		}
		return gqlResp.Data, fmt.Errorf("graphql errors: %s", strings.Join(msgs, "; "))
	}

	return gqlResp.Data, nil
}

// ProbeGraphQL checks if the Unraid server has the GraphQL API available.
// Returns true if the /graphql endpoint responds (even with auth errors).
func (c *Client) ProbeGraphQL(serverID string) bool {
	sess := c.getSession(serverID)
	if sess == nil {
		return false
	}

	gqlURL := sess.apiBase + "/graphql"
	transport := makeTransport()
	httpc := &http.Client{
		Timeout:   5 * time.Second,
		Transport: transport,
	}
	defer httpc.CloseIdleConnections()

	req, _ := http.NewRequest(http.MethodPost, gqlURL, strings.NewReader(`{"query":"{ online }"}`))
	req.Header.Set("Content-Type", "application/json")

	// Attach session cookies
	parsedURL, _ := url.Parse(gqlURL)
	if sess.jar != nil {
		for _, ck := range sess.jar.Cookies(parsedURL) {
			req.AddCookie(ck)
		}
	}

	resp, err := httpc.Do(req)
	if err != nil {
		logger.Debugf("graphql probe %s: %v", gqlURL, err)
		return false
	}
	defer resp.Body.Close()
	io.ReadAll(resp.Body)

	// 403 = sandbox disabled but API exists; 200 = working; 404 = no API
	if resp.StatusCode == http.StatusNotFound {
		logger.Debugf("graphql probe %s: 404 - API not available", gqlURL)
		return false
	}

	sess.graphqlAvailable = true
	logger.Infof("graphql probe %s: HTTP %d - API available", gqlURL, resp.StatusCode)
	return true
}

// HasGraphQL returns whether the GraphQL API has been detected for this server.
func (c *Client) HasGraphQL(serverID string) bool {
	sess := c.getSession(serverID)
	if sess == nil {
		return false
	}
	return sess.graphqlAvailable
}

// SetAPIKey sets an optional API key for GraphQL authentication.
func (c *Client) SetAPIKey(serverID, key string) {
	sess := c.getSession(serverID)
	if sess == nil {
		return
	}
	sess.apiKey = key
}

// ---------------------------------------------------------------------------
// GraphQL query strings (from Unraid official API schema)
// ---------------------------------------------------------------------------

const (
	// GetServer is a lightweight query for basic server info.
	QueryGetServer = `query GetServer {
  info {
    os { hostname uptime }
    versions { core { unraid } }
    machineId time
  }
  array { state }
  online
}`

	// GetSystemInfo returns detailed system information.
	QueryGetSystemInfo = `query GetSystemInfo {
  info {
    os { platform distro release codename kernel arch hostname logofile serial build uptime }
    cpu { manufacturer brand vendor family model stepping revision voltage speed speedmin speedmax threads cores processors socket cache }
    memory { layout { bank type clockSpeed formFactor manufacturer partNum serialNum } }
    baseboard { manufacturer model version serial assetTag }
    system { manufacturer model version serial uuid sku }
    versions { core { unraid api kernel } packages { openssl node npm pm2 git nginx php docker } }
    machineId time
  }
}`

	// GetMetrics returns current CPU and memory usage.
	QueryGetMetrics = `query GetMetrics {
  metrics {
    cpu { percentTotal }
    memory { total used free available buffcache percentTotal }
  }
}`

	// GetNetworkMetrics returns current network throughput.
	QueryGetNetworkMetrics = `query GetNetworkMetrics {
  metrics {
    network {
      id name operstate bytesReceived bytesSent
      packetsReceived packetsSent
      receiveErrors transmitErrors receiveDropped transmitDropped
      rxSec txSec utilizationPercent lastUpdated
    }
  }
}`

	// GetArrayStatus returns full array, disk, and capacity information.
	QueryGetArrayStatus = `query GetArrayStatus {
  array {
    id state
    capacity { kilobytes { free used total } disks { free used total } }
    boot { id idx name device size status rotational temp numReads numWrites numErrors fsSize fsFree fsUsed exportable type warning critical fsType comment format transport color }
    parities { id idx name device size status rotational temp numReads numWrites numErrors fsSize fsFree fsUsed exportable type warning critical fsType comment format transport color }
    disks { id idx name device size status rotational temp numReads numWrites numErrors fsSize fsFree fsUsed exportable type warning critical fsType comment format transport color }
    caches { id idx name device size status rotational temp numReads numWrites numErrors fsSize fsFree fsUsed exportable type warning critical fsType comment format transport color }
  }
}`

	// GetParityStatus returns current parity check progress.
	QueryGetParityStatus = `query GetParityStatus {
  array {
    parityCheckStatus { progress speed errors status paused running correcting }
  }
}`

	// ListDockerContainers returns all Docker containers.
	QueryListDockerContainers = `query ListDockerContainers {
  docker {
    containers { id names image state status autoStart }
  }
}`

	// GetContainerDetails returns detailed info for a single container.
	QueryGetContainerDetails = `query GetContainerDetails($id: PrefixedID!) {
  docker {
    container(id: $id) {
      id names image imageId command created
      ports { ip privatePort publicPort type }
      sizeRootFs labels state status
      hostConfig { networkMode }
      networkSettings mounts autoStart
    }
  }
}`

	// GetDockerContainerStats returns live container resource usage.
	QueryDockerContainerStats = `query GetDockerContainerStats {
  docker {
    containers { id names state }
  }
}`

	// GetDockerPorts returns all container port bindings.
	QueryGetDockerPorts = `query GetContainerPorts {
  docker {
    containers { id names state ports { ip privatePort publicPort type } }
  }
}`

	// ListVMs returns all virtual machines.
	QueryListVMs = `query ListVMs {
  vms {
    id domains { id name state uuid }
  }
}`

	// GetSharesInfo returns all user shares.
	QueryGetSharesInfo = `query GetSharesInfo {
  shares {
    name free used size include exclude cache nameOrig comment allocator splitLevel floor cow color luksStatus
  }
}`

	// GetServices returns running services.
	QueryGetServices = `query GetServices {
  services { name online version }
}`

	// GetVariables returns Unraid configuration variables.
	QueryGetVariables = `query GetSelectiveUnraidVariables {
  vars {
    id version name timeZone comment security workgroup domain domainShort
    hideDotFiles localMaster enableFruit useNtp domainLogin sysModel
    sysFlashSlots useSsl port portssl localTld bindMgt useTelnet porttelnet
    useSsh portssh startPage startArray shutdownTimeout
    shareSmbEnabled shareNfsEnabled shareAfpEnabled shareCacheEnabled
    shareAvahiEnabled safeMode startMode configValid configError joinStatus
    deviceCount flashGuid flashProduct flashVendor mdState mdVersion
    shareCount shareSmbCount shareNfsCount shareAfpCount shareMoverActive
  }
}`

	// ComprehensiveHealthCheck returns a combined health status.
	QueryComprehensiveHealth = `query ComprehensiveHealthCheck {
  info {
    time versions { core { unraid } }
    os { uptime }
  }
  array { state }
  notifications {
    overview { unread { alert warning total } }
  }
  docker {
    containers { id state status }
  }
}`

	// GetOnline is a simple reachability check.
	QueryGetOnline = `query GetOnline { online }`
)

// ---------------------------------------------------------------------------
// GraphQL mutation strings
// ---------------------------------------------------------------------------

const (
	// MutStartContainer starts a Docker container.
	MutStartContainer = `mutation StartContainer($id: PrefixedID!) {
  docker { start(id: $id) { id names state status } }
}`

	// MutStopContainer stops a Docker container.
	MutStopContainer = `mutation StopContainer($id: PrefixedID!) {
  docker { stop(id: $id) { id names state status } }
}`

	// MutRestartContainer restarts a Docker container.
	MutRestartContainer = `mutation RestartContainer($id: PrefixedID!) {
  docker { restart(id: $id) { id names state status } }
}`

	// MutStartArray starts the Unraid array.
	MutStartArray = `mutation StartArray {
  array { setState(input: { desiredState: START }) { state capacity { kilobytes { free used total } } } }
}`

	// MutStopArray stops the Unraid array.
	MutStopArray = `mutation StopArray {
  array { setState(input: { desiredState: STOP }) { state } }
}`

	// MutStartVM starts a virtual machine.
	MutStartVM = `mutation StartVM($id: PrefixedID!) {
  vm { start(id: $id) }
}`

	// MutStopVM stops a virtual machine.
	MutStopVM = `mutation StopVM($id: PrefixedID!) {
  vm { stop(id: $id) }
}`

	// MutForceStopVM force-stops a VM.
	MutForceStopVM = `mutation ForceStopVM($id: PrefixedID!) {
  vm { forceStop(id: $id) }
}`

	// MutPauseVM pauses a running VM.
	MutPauseVM = `mutation PauseVM($id: PrefixedID!) {
  vm { pause(id: $id) }
}`

	// MutResumeVM resumes a paused VM.
	MutResumeVM = `mutation ResumeVM($id: PrefixedID!) {
  vm { resume(id: $id) }
}`

	// MutRebootVM reboots a VM.
	MutRebootVM = `mutation RebootVM($id: PrefixedID!) {
  vm { reboot(id: $id) }
}`

	// MutResetVM hard-resets a VM.
	MutResetVM = `mutation ResetVM($id: PrefixedID!) {
  vm { reset(id: $id) }
}`
)

// ---------------------------------------------------------------------------
// Helper types for parsing GraphQL responses
// ---------------------------------------------------------------------------

// GQLInfo wraps the top-level "info" query response.
type GQLInfo struct {
	OS        *GQLOS     `json:"os,omitempty"`
	CPU       *GQLCPU    `json:"cpu,omitempty"`
	Memory    *GQLMemory `json:"memory,omitempty"`
	Baseboard *GQLBaseboard `json:"baseboard,omitempty"`
	System    *GQLSystem `json:"system,omitempty"`
	Versions  *GQLVersions `json:"versions,omitempty"`
	MachineID string     `json:"machineId,omitempty"`
	Time      string     `json:"time,omitempty"`
}

type GQLOS struct {
	Platform  string `json:"platform,omitempty"`
	Distro    string `json:"distro,omitempty"`
	Release   string `json:"release,omitempty"`
	Codename  string `json:"codename,omitempty"`
	Kernel    string `json:"kernel,omitempty"`
	Arch      string `json:"arch,omitempty"`
	Hostname  string `json:"hostname,omitempty"`
	Logofile  string `json:"logofile,omitempty"`
	Serial    string `json:"serial,omitempty"`
	Build     string `json:"build,omitempty"`
	Uptime    float64 `json:"uptime,omitempty"`
}

type GQLCPU struct {
	Manufacturer string  `json:"manufacturer,omitempty"`
	Brand        string  `json:"brand,omitempty"`
	Vendor       string  `json:"vendor,omitempty"`
	Family       string  `json:"family,omitempty"`
	Model        string  `json:"model,omitempty"`
	Speed        float64 `json:"speed,omitempty"`
	SpeedMin     float64 `json:"speedmin,omitempty"`
	SpeedMax     float64 `json:"speedmax,omitempty"`
	Threads      int     `json:"threads,omitempty"`
	Cores        int     `json:"cores,omitempty"`
	Processors   int     `json:"processors,omitempty"`
	Socket       string  `json:"socket,omitempty"`
	Cache        string  `json:"cache,omitempty"`
}

type GQLMemory struct {
	Layout []GQLMemoryBank `json:"layout,omitempty"`
}

type GQLMemoryBank struct {
	Bank       string `json:"bank,omitempty"`
	Type       string `json:"type,omitempty"`
	ClockSpeed string `json:"clockSpeed,omitempty"`
	FormFactor string `json:"formFactor,omitempty"`
	Manufacturer string `json:"manufacturer,omitempty"`
	PartNum    string `json:"partNum,omitempty"`
	SerialNum  string `json:"serialNum,omitempty"`
}

type GQLBaseboard struct {
	Manufacturer string `json:"manufacturer,omitempty"`
	Model        string `json:"model,omitempty"`
	Version      string `json:"version,omitempty"`
	Serial       string `json:"serial,omitempty"`
	AssetTag     string `json:"assetTag,omitempty"`
}

type GQLSystem struct {
	Manufacturer string `json:"manufacturer,omitempty"`
	Model        string `json:"model,omitempty"`
	Version      string `json:"version,omitempty"`
	Serial       string `json:"serial,omitempty"`
	UUID        string `json:"uuid,omitempty"`
	SKU         string `json:"sku,omitempty"`
}

type GQLVersions struct {
	Core     *GQLCoreVersions    `json:"core,omitempty"`
	Packages *GQLPackageVersions `json:"packages,omitempty"`
}

type GQLCoreVersions struct {
	Unraid string `json:"unraid,omitempty"`
	API    string `json:"api,omitempty"`
	Kernel string `json:"kernel,omitempty"`
}

type GQLPackageVersions struct {
	OpenSSL string `json:"openssl,omitempty"`
	Node    string `json:"node,omitempty"`
	NPM     string `json:"npm,omitempty"`
	PM2     string `json:"pm2,omitempty"`
	Git     string `json:"git,omitempty"`
	Nginx   string `json:"nginx,omitempty"`
	PHP     string `json:"php,omitempty"`
	Docker  string `json:"docker,omitempty"`
}

// GQLMetrics wraps the "metrics" query response.
type GQLMetrics struct {
	CPU    *GQLCPUMetrics    `json:"cpu,omitempty"`
	Memory *GQLMemoryMetrics `json:"memory,omitempty"`
}

type GQLCPUMetrics struct {
	PercentTotal float64 `json:"percentTotal,omitempty"`
}

type GQLMemoryMetrics struct {
	Total        int64   `json:"total,omitempty"`
	Used         int64   `json:"used,omitempty"`
	Free         int64   `json:"free,omitempty"`
	Available    int64   `json:"available,omitempty"`
	BuffCache    int64   `json:"buffcache,omitempty"`
	PercentTotal float64 `json:"percentTotal,omitempty"`
}

// GQLArray wraps the "array" query response.
type GQLArray struct {
	ID       string       `json:"id,omitempty"`
	State    string       `json:"state,omitempty"`
	Capacity *GQLCapacity `json:"capacity,omitempty"`
	Boot     *GQLDisk     `json:"boot,omitempty"`
	Parities []GQLDisk    `json:"parities,omitempty"`
	Disks    []GQLDisk    `json:"disks,omitempty"`
	Caches   []GQLDisk    `json:"caches,omitempty"`
	ParityCheckStatus *GQLParityCheck `json:"parityCheckStatus,omitempty"`
}

type GQLCapacity struct {
	Kilobytes *GQLCapacityValues `json:"kilobytes,omitempty"`
	Disks     *GQLCapacityValues  `json:"disks,omitempty"`
}

type GQLCapacityValues struct {
	Free  int64 `json:"free,omitempty"`
	Used  int64 `json:"used,omitempty"`
	Total int64 `json:"total,omitempty"`
}

type GQLDisk struct {
	ID         string `json:"id,omitempty"`
	Idx        int    `json:"idx,omitempty"`
	Name       string `json:"name,omitempty"`
	Device     string `json:"device,omitempty"`
	Size       string `json:"size,omitempty"`
	Status     string `json:"status,omitempty"`
	Rotational string `json:"rotational,omitempty"`
	Temp       string `json:"temp,omitempty"`
	NumReads   string `json:"numReads,omitempty"`
	NumWrites  string `json:"numWrites,omitempty"`
	NumErrors  string `json:"numErrors,omitempty"`
	FsSize     string `json:"fsSize,omitempty"`
	FsFree     string `json:"fsFree,omitempty"`
	FsUsed     string `json:"fsUsed,omitempty"`
	Exportable string `json:"exportable,omitempty"`
	Type       string `json:"type,omitempty"`
	Warning    string `json:"warning,omitempty"`
	Critical   string `json:"critical,omitempty"`
	FsType     string `json:"fsType,omitempty"`
	Comment    string `json:"comment,omitempty"`
	Format     string `json:"format,omitempty"`
	Transport  string `json:"transport,omitempty"`
	Color      string `json:"color,omitempty"`
}

type GQLParityCheck struct {
	Progress  float64 `json:"progress,omitempty"`
	Speed     float64 `json:"speed,omitempty"`
	Errors    int     `json:"errors,omitempty"`
	Status    string  `json:"status,omitempty"`
	Paused    bool    `json:"paused,omitempty"`
	Running   bool    `json:"running,omitempty"`
	Correcting bool   `json:"correcting,omitempty"`
}

// GQLDocker wraps the "docker" query response.
type GQLDocker struct {
	Containers []GQLContainer `json:"containers,omitempty"`
	Networks   []GQLNetwork   `json:"networks,omitempty"`
}

type GQLContainer struct {
	ID        string   `json:"id,omitempty"`
	Names     []string `json:"names,omitempty"`
	Image     string   `json:"image,omitempty"`
	ImageID   string   `json:"imageId,omitempty"`
	State     string   `json:"state,omitempty"`
	Status    string   `json:"status,omitempty"`
	AutoStart bool     `json:"autoStart,omitempty"`
	Command   string   `json:"command,omitempty"`
	Created   string   `json:"created,omitempty"`
	Ports     []GQLPort `json:"ports,omitempty"`
	Mounts    []GQLMount `json:"mounts,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
	HostConfig *GQLHostConfig `json:"hostConfig,omitempty"`
}

type GQLPort struct {
	IP          string `json:"ip,omitempty"`
	PrivatePort int    `json:"privatePort,omitempty"`
	PublicPort  int    `json:"publicPort,omitempty"`
	Type        string `json:"type,omitempty"`
}

type GQLMount struct {
	Source      string `json:"source,omitempty"`
	Destination string `json:"destination,omitempty"`
	Mode        string `json:"mode,omitempty"`
}

type GQLHostConfig struct {
	NetworkMode string `json:"networkMode,omitempty"`
}

type GQLNetwork struct {
	ID         string            `json:"id,omitempty"`
	Name       string            `json:"name,omitempty"`
	Driver     string            `json:"driver,omitempty"`
	Scope      string            `json:"scope,omitempty"`
	EnableIPv6 bool              `json:"enableIPv6,omitempty"`
	Internal   bool              `json:"internal,omitempty"`
	Attachable bool              `json:"attachable,omitempty"`
	Containers []GQLContainer   `json:"containers,omitempty"`
	Options    map[string]string `json:"options,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
}

// GQLVMs wraps the "vms" query response.
type GQLVMs struct {
	ID      string     `json:"id,omitempty"`
	Domains []GQLVMDomain `json:"domains,omitempty"`
}

type GQLVMDomain struct {
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	State string `json:"state,omitempty"`
	UUID  string `json:"uuid,omitempty"`
}

// GQLShare wraps a share from the "shares" query.
type GQLShare struct {
	Name        string `json:"name,omitempty"`
	Free        string `json:"free,omitempty"`
	Used        string `json:"used,omitempty"`
	Size        string `json:"size,omitempty"`
	Include     string `json:"include,omitempty"`
	Exclude     string `json:"exclude,omitempty"`
	Cache       string `json:"cache,omitempty"`
	NameOrig    string `json:"nameOrig,omitempty"`
	Comment     string `json:"comment,omitempty"`
	Allocator   string `json:"allocator,omitempty"`
	SplitLevel  string `json:"splitLevel,omitempty"`
	Floor       string `json:"floor,omitempty"`
	COW         string `json:"cow,omitempty"`
	Color       string `json:"color,omitempty"`
	LuksStatus  string `json:"luksStatus,omitempty"`
}

// GQLService wraps a service from the "services" query.
type GQLService struct {
	Name    string `json:"name,omitempty"`
	Online  bool   `json:"online,omitempty"`
	Version string `json:"version,omitempty"`
}

// GQLNetworkIface wraps a network interface from metrics.
type GQLNetworkIface struct {
	ID               string  `json:"id,omitempty"`
	Name             string  `json:"name,omitempty"`
	OperState        string  `json:"operstate,omitempty"`
	BytesReceived    int64   `json:"bytesReceived,omitempty"`
	BytesSent        int64   `json:"bytesSent,omitempty"`
	PacketsReceived  int64   `json:"packetsReceived,omitempty"`
	PacketsSent      int64   `json:"packetsSent,omitempty"`
	ReceiveErrors    int64   `json:"receiveErrors,omitempty"`
	TransmitErrors   int64   `json:"transmitErrors,omitempty"`
	ReceiveDropped   int64   `json:"receiveDropped,omitempty"`
	TransmitDropped  int64   `json:"transmitDropped,omitempty"`
	RxSec            float64 `json:"rxSec,omitempty"`
	TxSec            float64 `json:"txSec,omitempty"`
	UtilizationPct   float64 `json:"utilizationPercent,omitempty"`
	LastUpdated      string  `json:"lastUpdated,omitempty"`
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// parseGQLData is a generic helper to unmarshal a specific field from the
// GraphQL "data" raw message. Example: parseGQLData(raw, "info", &info)
func parseGQLData(data json.RawMessage, field string, target interface{}) error {
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return fmt.Errorf("parse graphql data wrapper: %w", err)
	}
	raw, ok := wrapper[field]
	if !ok {
		return fmt.Errorf("graphql response missing field %q", field)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("parse graphql field %q: %w", field, err)
	}
	return nil
}

// ParseInfoQuery parses the "info" field from a GetSystemInfo response.
func ParseInfoQuery(data json.RawMessage) (*GQLInfo, error) {
	var wrapper struct {
		Info GQLInfo `json:"info"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, fmt.Errorf("parse info: %w", err)
	}
	return &wrapper.Info, nil
}

// ParseMetricsQuery parses the "metrics" field from a GetMetrics response.
func ParseMetricsQuery(data json.RawMessage) (*GQLMetrics, error) {
	var wrapper struct {
		Metrics GQLMetrics `json:"metrics"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, fmt.Errorf("parse metrics: %w", err)
	}
	return &wrapper.Metrics, nil
}

// ParseArrayQuery parses the "array" field from a GetArrayStatus response.
func ParseArrayQuery(data json.RawMessage) (*GQLArray, error) {
	var wrapper struct {
		Array GQLArray `json:"array"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, fmt.Errorf("parse array: %w", err)
	}
	return &wrapper.Array, nil
}

// ParseDockerQuery parses the "docker" field from a ListDockerContainers response.
func ParseDockerQuery(data json.RawMessage) (*GQLDocker, error) {
	var wrapper struct {
		Docker GQLDocker `json:"docker"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, fmt.Errorf("parse docker: %w", err)
	}
	return &wrapper.Docker, nil
}

// ParseVMsQuery parses the "vms" field from a ListVMs response.
func ParseVMsQuery(data json.RawMessage) ([]GQLVMs, error) {
	var wrapper struct {
		VMs []GQLVMs `json:"vms"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, fmt.Errorf("parse vms: %w", err)
	}
	return wrapper.VMs, nil
}
