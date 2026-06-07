// Package docker discovers dev servers/databases published by Docker containers
// and attributes compose services back to their repo via compose labels.
package docker

import (
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/jmcadams/ports/internal/model"
	"github.com/jmcadams/ports/internal/project"
)

// confidence levels for container-backed servers.
const (
	confCompose   = 80 // compose service attributed to a repo working_dir
	confContainer = 40 // standalone container, no repo attribution
)

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
	if workdir == "" {
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
	port  int
	proto string
}

// hostPorts returns the deduped set of published host ports for a container.
// A single container port often binds both IPv4 and IPv6 to the same host port.
func hostPorts(c inspect) []portMap {
	seen := map[string]bool{}
	var out []portMap
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
			if seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, portMap{port: p, proto: proto})
		}
	}
	return out
}

func runningIDs() ([]string, error) {
	out, err := exec.Command("docker", "ps", "-q", "--no-trunc").Output()
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(out)), nil
}

func inspectAll(ids []string) ([]inspect, error) {
	args := append([]string{"inspect"}, ids...)
	out, err := exec.Command("docker", args...).Output()
	if err != nil {
		return nil, err
	}
	var containers []inspect
	if err := json.Unmarshal(out, &containers); err != nil {
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
