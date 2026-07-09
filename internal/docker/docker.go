// Package docker discovers dev servers/databases published by Docker containers
// and attributes compose services back to their repo via compose labels.
package docker

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joshmcadams/whence/internal/execx"
	"github.com/joshmcadams/whence/internal/model"
	"github.com/joshmcadams/whence/internal/project"
)

// confidence levels for container-backed servers.
const (
	confCompose   = 80 // compose service attributed to a repo working_dir
	confContainer = 40 // standalone container, no repo attribution
)

// dockerTimeout bounds each docker CLI call. Docker is best-effort, so a wedged
// daemon must time out rather than hang `whence list`; 5s tolerates a slow but
// healthy daemon (e.g. the first query after boot) without dropping results.
const dockerTimeout = 5 * time.Second

// Available reports whether the docker CLI is usable on this machine.
func Available() bool {
	_, err := exec.LookPath("docker")
	return err == nil
}

type inspect struct {
	Name  string `json:"Name"`
	State struct {
		StartedAt string `json:"StartedAt"`
	} `json:"State"`
	Config struct {
		Image  string            `json:"Image"`
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	NetworkSettings struct {
		Ports map[string][]struct {
			HostIP   string `json:"HostIp"`
			HostPort string `json:"HostPort"`
		} `json:"Ports"`
	} `json:"NetworkSettings"`
}

// Servers returns one model.Server per published host port of each running
// container. Kubernetes-managed containers (io.kubernetes.* labels or k8s_*
// names) are skipped as infrastructure. If docker is unavailable, returns nil.
func Servers() ([]model.Server, error) {
	if !Available() {
		return nil, nil
	}
	ids, err := runningIDs()
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	containers, err := inspectAll(ids)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	var servers []model.Server
	for _, c := range containers {
		if isKubernetes(c) {
			continue
		}
		name := strings.TrimPrefix(c.Name, "/")
		start := parseTime(c.State.StartedAt)

		proj, conf := classifyContainer(c)

		for _, hp := range hostPorts(c) {
			s := model.Server{
				Port:       hp.port,
				Proto:      hp.proto,
				Address:    hp.address,
				Source:     model.SourceDocker,
				Name:       name,
				Cmdline:    c.Config.Image,
				Project:    proj,
				Confidence: conf,
			}
			if !start.IsZero() {
				s.StartTime = start
				s.Uptime = now.Sub(start)
			}
			servers = append(servers, s)
		}
	}
	return servers, nil
}

func classifyContainer(c inspect) (*model.Project, int) {
	labels := c.Config.Labels
	workdir := labels["com.docker.compose.project.working_dir"]
	if workdir == "" || !isLocalDir(workdir) {
		return nil, confContainer
	}
	name := labels["com.docker.compose.project"]
	if name == "" {
		name = strings.TrimPrefix(c.Name, "/")
	}
	return &model.Project{
		Name:        name,
		Root:        workdir,
		Description: project.Description(workdir),
		Marker:      "docker-compose",
	}, confCompose
}

// isLocalDir reports whether p is an absolute path to a directory that
// exists on this machine. The compose working_dir label is an arbitrary
// string any container can carry — this is the spoof-resistance floor for
// granting compose attribution (confidence 80): the label must point at a
// real directory here, not just anywhere. It deliberately does not check
// IsUnderDevRoot (compose projects outside dev roots are legitimate) and
// uses Stat, not Lstat (a compose project checked out via a symlinked path
// is legitimate — the guard's job is "does this resolve to a real
// directory", which also naturally fails for containers built elsewhere).
func isLocalDir(p string) bool {
	if !filepath.IsAbs(p) {
		return false
	}
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func isKubernetes(c inspect) bool {
	if strings.HasPrefix(strings.TrimPrefix(c.Name, "/"), "k8s_") {
		return true
	}
	for k := range c.Config.Labels {
		if strings.HasPrefix(k, "io.kubernetes.") {
			return true
		}
	}
	return false
}

type portMap struct {
	port    int
	proto   string
	address string
}

// hostPorts returns the deduped set of published host ports for a container,
// capturing the bind address. When a port is bound to both IPv4 and IPv6
// all-interfaces addresses, the address is normalised to "0.0.0.0".
func hostPorts(c inspect) []portMap {
	acc := map[string]*portMap{}
	var order []string
	for key, binds := range c.NetworkSettings.Ports {
		proto := "tcp"
		if i := strings.IndexByte(key, '/'); i >= 0 {
			if strings.HasSuffix(key, "/udp") {
				continue // only listening TCP services
			}
			proto = key[i+1:]
		}
		for _, b := range binds {
			if b.HostPort == "" {
				continue
			}
			p, err := strconv.Atoi(b.HostPort)
			if err != nil {
				continue
			}
			k := strconv.Itoa(p) + "/" + proto
			if e, ok := acc[k]; ok {
				// Upgrade to all-interfaces if any binding is open to all.
				if isAllInterfacesIP(b.HostIP) && !isAllInterfacesIP(e.address) {
					e.address = "0.0.0.0"
				}
			} else {
				addr := b.HostIP
				if isAllInterfacesIP(addr) {
					addr = "0.0.0.0"
				}
				acc[k] = &portMap{port: p, proto: proto, address: addr}
				order = append(order, k)
			}
		}
	}
	out := make([]portMap, 0, len(order))
	for _, k := range order {
		out = append(out, *acc[k])
	}
	return out
}

func isAllInterfacesIP(ip string) bool {
	return model.IsAllInterfaces(ip)
}

// dockerOutput seams the exec call so tests can simulate exit codes/stdout
// without a real docker daemon.
var dockerOutput = execx.Output

func runningIDs() ([]string, error) {
	out, err := dockerOutput(dockerTimeout, "docker", "ps", "-q", "--no-trunc")
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(out)), nil
}

// inspectAll parses stdout even when the command exited non-zero: a
// container that exits between `docker ps -q` and this call makes `docker
// inspect` exit 1, but it still prints the JSON array of the containers it
// did find on stdout (errors go to stderr). Treat that as a partial success
// rather than discarding every row for the cycle.
func inspectAll(ids []string) ([]inspect, error) {
	args := append([]string{"inspect", "--"}, ids...)
	out, err := dockerOutput(dockerTimeout, "docker", args...)
	var containers []inspect
	if jsonErr := json.Unmarshal(out, &containers); jsonErr == nil && len(containers) > 0 {
		return containers, nil // partial success: some ids resolved before exit 1
	}
	if err != nil {
		return nil, err
	}
	return containers, nil
}

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
