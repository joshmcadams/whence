package docker

import (
	"sort"
	"testing"
)

func TestHostPorts_DedupesV4V6AndSkipsUDP(t *testing.T) {
	var c inspect
	c.NetworkSettings.Ports = map[string][]struct {
		HostIP   string `json:"HostIp"`
		HostPort string `json:"HostPort"`
	}{
		"5432/tcp": {{HostIP: "0.0.0.0", HostPort: "5433"}, {HostIP: "::", HostPort: "5433"}},
		"53/udp":   {{HostIP: "0.0.0.0", HostPort: "5300"}},
		"80/tcp":   {{HostIP: "0.0.0.0", HostPort: "8080"}},
	}

	got := hostPorts(c)
	ports := make([]int, 0, len(got))
	for _, p := range got {
		ports = append(ports, p.port)
	}
	sort.Ints(ports)

	if len(ports) != 2 {
		t.Fatalf("got %d ports %v, want 2 (5433, 8080)", len(ports), ports)
	}
	if ports[0] != 5433 || ports[1] != 8080 {
		t.Errorf("ports = %v, want [5433 8080]", ports)
	}
}

func TestClassifyContainer(t *testing.T) {
	var c inspect
	c.Name = "/jfdid-db-1"
	c.Config.Labels = map[string]string{
		"com.docker.compose.project":             "jfdid",
		"com.docker.compose.project.working_dir": "/home/me/dev/jfdid",
	}
	proj, conf := classifyContainer(c)
	if proj == nil || proj.Name != "jfdid" || proj.Root != "/home/me/dev/jfdid" {
		t.Fatalf("project = %+v", proj)
	}
	if conf != confCompose {
		t.Errorf("conf = %d, want %d", conf, confCompose)
	}

	// No compose labels -> no project, lower confidence.
	var bare inspect
	bare.Name = "/redis"
	if proj, conf := classifyContainer(bare); proj != nil || conf != confContainer {
		t.Errorf("bare container: proj=%+v conf=%d", proj, conf)
	}
}

func TestIsKubernetes(t *testing.T) {
	var byName inspect
	byName.Name = "/k8s_coredns_x"
	if !isKubernetes(byName) {
		t.Error("k8s_ name should be detected")
	}

	var byLabel inspect
	byLabel.Name = "/something"
	byLabel.Config.Labels = map[string]string{"io.kubernetes.pod.name": "p"}
	if !isKubernetes(byLabel) {
		t.Error("io.kubernetes.* label should be detected")
	}

	var compose inspect
	compose.Name = "/jfdid-db-1"
	compose.Config.Labels = map[string]string{"com.docker.compose.project": "jfdid"}
	if isKubernetes(compose) {
		t.Error("compose container should not be flagged as k8s")
	}
}
