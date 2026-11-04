// Package operators pre-installs Kubernetes operators that sdcore v6.x
// requires (MongoDB Community Operator, Strimzi Kafka Operator) and
// their prerequisites (local-path-provisioner StorageClass). The
// aether-onramp Ansible skips operator installation in airgap mode;
// this component fills that gap from the bundled Helm charts. See
// issue #76 for the proper upstream fix.
package operators

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components"
	"github.com/aether-gui/aether-ops-bootstrap/internal/state"
)

const (
	helmBin   = "/usr/local/bin/helm"
	kubectlBin = "/var/lib/rancher/rke2/bin/kubectl"
	kubeconfig = "/etc/rancher/rke2/rke2.yaml"

	namespace = "aether-5gc"

	mongoChartName  = "mongodb-kubernetes"
	strimziChartName = "strimzi-kafka-operator"

	lppManifestRel = "templates/operators/local-path-storage.yaml"
)

type Component struct {
	extractDir string
	manifest   *bundle.Manifest
}

func New(extractDir string, manifest *bundle.Manifest) *Component {
	return &Component{extractDir: extractDir, manifest: manifest}
}

func (c *Component) Name() string { return "operators" }

func (c *Component) DesiredVersion(b *bundle.Manifest) string {
	if !c.hasOperatorCharts() {
		return ""
	}
	var parts []string
	for _, hc := range b.Components.HelmCharts {
		if hc.Name == mongoChartName || hc.Name == strimziChartName {
			parts = append(parts, hc.Name+"="+hc.ResolvedSHA[:min(12, len(hc.ResolvedSHA))])
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ",")
}

func (c *Component) CurrentVersion(s *state.State) string {
	if cs, ok := s.Components["operators"]; ok {
		return cs.Version
	}
	return ""
}

func (c *Component) hasOperatorCharts() bool {
	if c.manifest == nil {
		return false
	}
	for _, hc := range c.manifest.Components.HelmCharts {
		if hc.Name == mongoChartName || hc.Name == strimziChartName {
			return true
		}
	}
	return false
}

func (c *Component) Plan(current, desired string) (components.Plan, error) {
	if desired == "" {
		return components.Plan{NoOp: true}, nil
	}
	if current == desired {
		return components.Plan{NoOp: true}, nil
	}

	var actions []components.Action

	// local-path-provisioner for MongoDB PVCs.
	lppPath := filepath.Join(c.extractDir, lppManifestRel)
	if _, err := os.Stat(lppPath); err == nil {
		actions = append(actions, components.Action{
			Description: "install local-path-provisioner StorageClass",
			Fn: func(ctx context.Context) error {
				return applyLocalPathProvisioner(ctx, lppPath)
			},
		})
	}

	for _, hc := range c.manifest.Components.HelmCharts {
		hc := hc
		switch hc.Name {
		case mongoChartName:
			chartDir := filepath.Join(c.extractDir, hc.Path, "helm_chart")
			actions = append(actions, components.Action{
				Description: fmt.Sprintf("install %s operator from bundled chart", mongoChartName),
				Fn: func(ctx context.Context) error {
					return installMongoDBOperator(ctx, chartDir)
				},
			})
		case strimziChartName:
			chartDir := filepath.Join(c.extractDir, hc.Path, "helm-charts", "helm3", "strimzi-kafka-operator")
			actions = append(actions, components.Action{
				Description: fmt.Sprintf("install %s from bundled chart", strimziChartName),
				Fn: func(ctx context.Context) error {
					return installStrimziOperator(ctx, chartDir)
				},
			})
		}
	}

	if len(actions) == 0 {
		return components.Plan{NoOp: true}, nil
	}
	return components.Plan{Actions: actions}, nil
}

func (c *Component) Apply(ctx context.Context, plan components.Plan) error {
	return components.ApplyPlan(ctx, c.Name(), plan)
}

func applyLocalPathProvisioner(ctx context.Context, manifestPath string) error {
	if hasDefaultStorageClass(ctx) {
		log.Printf("  default StorageClass already exists; skipping local-path-provisioner")
		return nil
	}

	log.Printf("  applying local-path-provisioner manifest...")
	cmd := exec.CommandContext(ctx, kubectlBin,
		"--kubeconfig", kubeconfig,
		"apply", "-f", manifestPath, "--wait=true")
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("applying local-path-provisioner: %w\n%s", err, out)
	}

	log.Printf("  patching local-path StorageClass as default...")
	patch := exec.CommandContext(ctx, kubectlBin,
		"--kubeconfig", kubeconfig,
		"patch", "storageclass", "local-path", "-p",
		`{"metadata": {"annotations":{"storageclass.kubernetes.io/is-default-class":"true"}}}`)
	patch.Env = os.Environ()
	out, err = patch.CombinedOutput()
	if err != nil {
		return fmt.Errorf("patching StorageClass as default: %w\n%s", err, out)
	}

	return nil
}

func hasDefaultStorageClass(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, kubectlBin,
		"--kubeconfig", kubeconfig,
		"get", "storageclass", "-o",
		`jsonpath={.items[?(@.metadata.annotations.storageclass\.kubernetes\.io/is-default-class=="true")].metadata.name}`)
	cmd.Env = os.Environ()
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return len(bytes.TrimSpace(out)) > 0
}

func installMongoDBOperator(ctx context.Context, chartDir string) error {
	if _, err := os.Stat(chartDir); err != nil {
		return fmt.Errorf("mongodb operator chart not found at %s: %w", chartDir, err)
	}

	log.Printf("  creating namespace %s...", namespace)
	ensureNamespace(ctx, namespace)

	log.Printf("  installing mongodb-kubernetes operator from %s...", chartDir)
	cmd := exec.CommandContext(ctx, helmBin,
		"--kubeconfig", kubeconfig,
		"upgrade", "--install", "mongodb-kubernetes", chartDir,
		"--namespace", namespace,
		"--set", "operator.watchNamespace="+namespace,
		"--wait", "--timeout", "5m0s")
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("installing mongodb-kubernetes operator: %w\n%s", err, out)
	}
	log.Printf("  mongodb-kubernetes operator installed")

	return waitForDeployment(ctx, namespace, "mongodb-kubernetes-operator", 3*time.Minute)
}

func installStrimziOperator(ctx context.Context, chartDir string) error {
	if _, err := os.Stat(chartDir); err != nil {
		return fmt.Errorf("strimzi operator chart not found at %s: %w", chartDir, err)
	}

	log.Printf("  creating namespace %s...", namespace)
	ensureNamespace(ctx, namespace)

	log.Printf("  installing strimzi-kafka-operator from %s...", chartDir)
	cmd := exec.CommandContext(ctx, helmBin,
		"--kubeconfig", kubeconfig,
		"upgrade", "--install", "strimzi-kafka-operator", chartDir,
		"--namespace", namespace,
		"--wait", "--timeout", "5m0s")
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("installing strimzi-kafka-operator: %w\n%s", err, out)
	}
	log.Printf("  strimzi-kafka-operator installed")

	return waitForDeployment(ctx, namespace, "strimzi-cluster-operator", 3*time.Minute)
}

func ensureNamespace(ctx context.Context, ns string) {
	cmd := exec.CommandContext(ctx, kubectlBin,
		"--kubeconfig", kubeconfig,
		"create", "namespace", ns)
	cmd.Env = os.Environ()
	_ = cmd.Run() // ignore "already exists"
}

func waitForDeployment(ctx context.Context, ns, name string, timeout time.Duration) error {
	log.Printf("  waiting for deployment %s/%s to be ready...", ns, name)
	cmd := exec.CommandContext(ctx, kubectlBin,
		"--kubeconfig", kubeconfig,
		"rollout", "status", "deployment/"+name,
		"--namespace", ns,
		"--timeout", fmt.Sprintf("%.0fs", timeout.Seconds()))
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("waiting for %s: %w\n%s", name, err, out)
	}
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
