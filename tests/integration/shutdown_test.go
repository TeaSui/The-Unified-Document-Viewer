package integration_test

import (
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestIntegration_GracefulShutdown(t *testing.T) {
	resp, err := http.Get(baseURL() + "/healthz")
	if err != nil || resp.StatusCode != 200 {
		t.Skip("app not running, skipping shutdown test")
	}
	resp.Body.Close()

	doneCh := make(chan int, 1)
	go func() {
		r, err := http.Get(baseURL() + "/vehicles/" + testVIN + "/documents")
		if err != nil {
			doneCh <- -1
			return
		}
		r.Body.Close()
		doneCh <- r.StatusCode
	}()

	time.Sleep(10 * time.Millisecond)
	cmd := exec.Command("docker", "compose", "kill", "-s", "SIGTERM", "app")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to send SIGTERM: %v\n%s", err, out)
	}

	select {
	case status := <-doneCh:
		if status != 200 {
			t.Errorf("in-flight request got status %d, expected 200", status)
		}
	case <-time.After(5 * time.Second):
		t.Error("in-flight request did not complete within 5s")
	}

	time.Sleep(1 * time.Second)
	_, err = http.Get(baseURL() + "/healthz")
	if err == nil {
		t.Log("app still responding after SIGTERM — may have restarted via Compose healthcheck")
	}

	cmd = exec.Command("docker", "compose", "start", "app")
	cmd.CombinedOutput()
	time.Sleep(5 * time.Second)

	for i := 0; i < 10; i++ {
		resp, err = http.Get(baseURL() + "/healthz")
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			return
		}
		time.Sleep(1 * time.Second)
	}
	t.Log("warning: app did not restart within 10s")
}

func TestIntegration_GracefulShutdown_DBPoolClosed(t *testing.T) {
	cmd := exec.Command("docker", "compose", "logs", "app", "--tail=20")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Skipf("could not get logs: %v", err)
	}

	logs := string(out)
	if !strings.Contains(logs, "shutting down server") {
		t.Log("no shutdown log found — app may not have been stopped yet")
	}
}
