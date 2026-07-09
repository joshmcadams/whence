package docker

import (
	"encoding/json"
	"errors"
	"os"
	"sort"
	"testing"
	"time"
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

func TestHostPorts_CapturesAddress(t *testing.T) {
	var c inspect
	c.NetworkSettings.Ports = map[string][]struct {
		HostIP   string `json:"HostIp"`
		HostPort string `json:"HostPort"`
	}{
		// Loopback-only binding.
		"5432/tcp": {{HostIP: "127.0.0.1", HostPort: "5433"}},
		// All-interfaces: two bindings (v4 + v6) for the same host port.
		"6379/tcp": {{HostIP: "0.0.0.0", HostPort: "6379"}, {HostIP: "::", HostPort: "6379"}},
	}

	got := hostPorts(c)
	addrs := map[int]string{}
	for _, p := range got {
		addrs[p.port] = p.address
	}

	if addrs[5433] != "127.0.0.1" {
		t.Errorf("port 5433 address = %q, want 127.0.0.1", addrs[5433])
	}
	if addrs[6379] != "0.0.0.0" {
		t.Errorf("port 6379 address = %q, want 0.0.0.0 (all-interfaces)", addrs[6379])
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

func withDockerOutput(t *testing.T, fn func(timeout time.Duration, name string, args ...string) ([]byte, error)) {
	orig := dockerOutput
	dockerOutput = fn
	t.Cleanup(func() { dockerOutput = orig })
}

func TestInspectAll_PartialSuccessOnNonZeroExit(t *testing.T) {
	// docker inspect exits 1 because one id vanished, but still printed the
	// JSON array of the containers it did find on stdout.
	valid := []byte(`[{"Name":"/found"}]`)
	withDockerOutput(t, func(timeout time.Duration, name string, args ...string) ([]byte, error) {
		return valid, errors.New("exit status 1")
	})

	got, err := inspectAll([]string{"found", "gone"})
	if err != nil {
		t.Fatalf("err = %v, want nil (partial success)", err)
	}
	if len(got) != 1 || got[0].Name != "/found" {
		t.Fatalf("got = %+v, want one container named /found", got)
	}
}

func TestInspectAll_EmptyOutputPropagatesError(t *testing.T) {
	// A real timeout or daemon failure yields empty/invalid stdout; the
	// error path must be preserved.
	wantErr := errors.New("docker timed out after 5s")
	withDockerOutput(t, func(timeout time.Duration, name string, args ...string) ([]byte, error) {
		return nil, wantErr
	})

	got, err := inspectAll([]string{"x"})
	if err == nil {
		t.Fatal("err = nil, want propagated error")
	}
	if got != nil {
		t.Fatalf("got = %+v, want nil", got)
	}
}

func TestInspectAll_AllIDsUnknownPropagatesError(t *testing.T) {
	// An all-ids-unknown run prints "[]" (empty slice) and exits non-zero —
	// falls through to the error path, not treated as success.
	wantErr := errors.New("exit status 1")
	withDockerOutput(t, func(timeout time.Duration, name string, args ...string) ([]byte, error) {
		return []byte(`[]`), wantErr
	})

	got, err := inspectAll([]string{"gone"})
	if err == nil {
		t.Fatal("err = nil, want propagated error")
	}
	if got != nil {
		t.Fatalf("got = %+v, want nil", got)
	}
}

func TestInspectAll_NormalPathOnSuccess(t *testing.T) {
	valid := []byte(`[{"Name":"/a"},{"Name":"/b"}]`)
	withDockerOutput(t, func(timeout time.Duration, name string, args ...string) ([]byte, error) {
		return valid, nil
	})

	got, err := inspectAll([]string{"a", "b"})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if len(got) != 2 {
		t.Fatalf("got = %+v, want 2 containers", got)
	}
}

// TestInspectJSONContract exercises the real `inspect` JSON tags against a
// fixture shaped like real `docker inspect` output, so a wrong tag (e.g.
// HostIp vs HostIP) fails here instead of only in production.
func TestInspectJSONContract(t *testing.T) {
	raw, err := os.ReadFile("testdata/inspect.json")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var containers []inspect
	if err := json.Unmarshal(raw, &containers); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(containers) != 3 {
		t.Fatalf("got %d containers, want 3", len(containers))
	}
	c1, c2, c3 := containers[0], containers[1], containers[2]

	if !isKubernetes(c2) {
		t.Errorf("isKubernetes(c2) = false, want true")
	}

	hp := hostPorts(c1)
	if len(hp) != 1 {
		t.Fatalf("hostPorts(c1) = %+v, want exactly one entry", hp)
	}
	if hp[0].port != 5433 || hp[0].proto != "tcp" || hp[0].address != "0.0.0.0" {
		t.Errorf("hostPorts(c1)[0] = %+v, want {5433 tcp 0.0.0.0}", hp[0])
	}

	// c3's ipv6 all-interfaces bind ("::") normalizes to "0.0.0.0".
	hp3 := hostPorts(c3)
	if len(hp3) != 1 || hp3[0].address != "0.0.0.0" {
		t.Errorf("hostPorts(c3) = %+v, want one entry with address 0.0.0.0", hp3)
	}

	if parseTime(c1.State.StartedAt).IsZero() {
		t.Error("parseTime(c1.State.StartedAt) is zero, want non-zero")
	}
	if !parseTime("garbage").IsZero() {
		t.Error("parseTime(\"garbage\") is non-zero, want zero")
	}

	proj, conf := classifyContainer(c1)
	if proj == nil || proj.Name != "jfdid" || proj.Marker != "docker-compose" {
		t.Fatalf("classifyContainer(c1) = %+v, %d; want project named jfdid, marker docker-compose", proj, conf)
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
