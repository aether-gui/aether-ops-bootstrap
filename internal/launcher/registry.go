package launcher

import (
	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components/aetherops"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components/debs"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components/helm"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components/onramp"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components/rke2"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components/serviceaccount"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components/ssh"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components/sudoers"
	"github.com/aether-gui/aether-ops-bootstrap/internal/components/wheelhouse"
)

// BuildRegistry creates the component registry in dependency order.
// extractDir is the path to the extracted bundle contents.
func BuildRegistry(extractDir string, manifest *bundle.Manifest, suite string) *components.Registry {
	r := &components.Registry{}

	// Order matters: each component may depend on the ones before it.
	debComp := debs.New(extractDir, manifest)
	debComp.SetSuite(suite)
	r.Register(debComp)
	sshComp := ssh.New(extractDir)
	sshComp.SetManifest(manifest)
	r.Register(sshComp)
	sudoersComp := sudoers.New(extractDir)
	sudoersComp.SetManifest(manifest)
	r.Register(sudoersComp)
	svcComp := serviceaccount.New()
	svcComp.SetManifest(manifest)
	r.Register(svcComp)
	r.Register(wheelhouse.New(extractDir, manifest))
	r.Register(rke2.New(extractDir, manifest))
	r.Register(helm.New(extractDir, manifest))
	// onramp extracts the bundled Ansible toolchain and helm charts so
	// the aether-ops daemon has its content in place before it starts.
	r.Register(onramp.New(extractDir, manifest))
	r.Register(aetherops.New(extractDir, manifest))

	return r
}
