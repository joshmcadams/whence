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

func TestClassifyContainer_DockerComposeLabels(t *testing.T) {
	workdir := t.TempDir()

	var c inspect
	c.Name = "/jfdid-db-1"
	c.Config.Labels = map[string]string{
		"com.docker.compose.project":             "jfdid",
		"com.docker.compose.project.working_dir": workdir,
	}
	proj, conf := classifyContainer(c)
	if proj == nil || proj.Name != "jfdid" || proj.Root != workdir || proj.Marker != "docker-compose" {
		t.Fatalf("project = %+v", proj)
	}
	if conf != confCompose {
		t.Errorf("conf = %d, want %d", conf, confCompose)
	}
}

func TestClassifyContainer_PodmanComposeLabels(t *testing.T) {
	workdir := t.TempDir()

	var c inspect
	c.Name = "/myapp-web-1"
	c.Config.Labels = map[string]string{
		"io.podman.compose.project":             "myapp",
		"io.podman.compose.project.working_dir": workdir,
	}
	proj, conf := classifyContainer(c)
	if proj == nil || proj.Name != "myapp" || proj.Root != workdir || proj.Marker != "podman-compose" {
		t.Fatalf("project = %+v", proj)
	}
	if conf != confCompose {
		t.Errorf("conf = %d, want %d", conf, confCompose)
	}
}

func TestClassifyContainer_BothNamespacesDockerWins(t *testing.T) {
	dockerWorkdir := t.TempDir()
	podmanWorkdir := t.TempDir()

	var c inspect
	c.Name = "/both-1"
	c.Config.Labels = map[string]string{
		"com.docker.compose.project":             "docker-app",
		"com.docker.compose.project.working_dir": dockerWorkdir,
		"io.podman.compose.project":              "podman-app",
		"io.podman.compose.project.working_dir":  podmanWorkdir,
	}
	proj, conf := classifyContainer(c)
	if proj == nil {
		t.Fatal("project is nil, want docker-namespace attribution")
	}
	if proj.Name != "docker-app" {
		t.Errorf("Name = %q, want %q (docker namespace wins)", proj.Name, "docker-app")
	}
	if proj.Root != dockerWorkdir {
		t.Errorf("Root = %q, want %q (docker namespace wins)", proj.Root, dockerWorkdir)
	}
	if proj.Marker != "docker-compose" {
		t.Errorf("Marker = %q, want %q", proj.Marker, "docker-compose")
	}
	if conf != confCompose {
		t.Errorf("conf = %d, want %d", conf, confCompose)
	}
}

func TestClassifyContainer_NoComposeLabelsIsStandalone(t *testing.T) {
	var c inspect
	c.Name = "/redis"
	c.Config.Labels = map[string]string{}
	if proj, conf := classifyContainer(c); proj != nil || conf != confContainer {
		t.Errorf("no compose labels: proj=%+v conf=%d", proj, conf)
	}
}

// TestClassifyContainer_WorkdirMustExistLocally guards against label
// name-squatting: a compose working_dir label is an arbitrary string any
// container can carry, so attribution must be denied unless it resolves to a
// real directory on this machine.
func TestClassifyContainer_WorkdirMustExistLocally(t *testing.T) {
	cases := []struct {
		name    string
		workdir string
	}{
		{"nonexistent absolute path", "/nonexistent/xyz"},
		{"relative path", "../../etc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var c inspect
			c.Name = "/spoofed-1"
			c.Config.Labels = map[string]string{
				"com.docker.compose.project":             "spoofed",
				"com.docker.compose.project.working_dir": tc.workdir,
			}
			proj, conf := classifyContainer(c)
			if proj != nil {
				t.Errorf("project = %+v, want nil", proj)
			}
			if conf != confContainer {
				t.Errorf("conf = %d, want %d", conf, confContainer)
			}
		})
	}
}

func TestIsLocalDir(t *testing.T) {
	real := t.TempDir()
	if !isLocalDir(real) {
		t.Errorf("isLocalDir(%q) = false, want true", real)
	}
	if isLocalDir("/nonexistent/xyz") {
		t.Error("isLocalDir(nonexistent) = true, want false")
	}
	if isLocalDir("../../etc") {
		t.Error("isLocalDir(relative) = true, want false")
	}
	if isLocalDir("") {
		t.Error("isLocalDir(empty) = true, want false")
	}
	// A file, not a directory, must also be rejected.
	file := real + string(os.PathSeparator) + "f"
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if isLocalDir(file) {
		t.Error("isLocalDir(regular file) = true, want false")
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

	got, err := inspectAll("docker", []string{"found", "gone"})
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

	got, err := inspectAll("docker", []string{"x"})
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

	got, err := inspectAll("docker", []string{"gone"})
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

	got, err := inspectAll("docker", []string{"a", "b"})
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

	// Confirm the fixture still carries the compose labels this JSON contract
	// depends on (a label-tag regression would silently zero this out).
	if got := c1.Config.Labels["com.docker.compose.project.working_dir"]; got != "/home/me/dev/jfdid" {
		t.Fatalf("c1 working_dir label = %q, want /home/me/dev/jfdid", got)
	}
	// That working_dir does not exist on the machine running this test, so
	// classifyContainer correctly denies compose attribution (see
	// TestClassifyContainer for the real-directory success path and
	// TestClassifyContainer_WorkdirMustExistLocally for the nonexistent-path
	// guard this exercises incidentally).
	proj, conf := classifyContainer(c1)
	if proj != nil || conf != confContainer {
		t.Fatalf("classifyContainer(c1) = %+v, %d; want nil project, confContainer (fixture working_dir doesn't exist on this machine)", proj, conf)
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
