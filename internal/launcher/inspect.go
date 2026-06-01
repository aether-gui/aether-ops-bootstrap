package launcher

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/aether-gui/aether-ops-bootstrap/internal/archive"
	"github.com/aether-gui/aether-ops-bootstrap/internal/bundle"
)

// InspectOpts configures a bundle inspection run.
type InspectOpts struct {
	// BundlePath is the path to a bundle.tar.zst on disk.
	BundlePath string
	// JSON requests machine-readable output (the parsed manifest).
	JSON bool
	// Out receives the rendered output. Required; the CLI passes os.Stdout.
	Out io.Writer
}

// Inspect prints a summary of a bundle: version, build provenance,
// component pins, applied patches, and outer-archive integrity. It
// streams the in-bundle manifest from the tarball without extracting
// the rest of the bundle to disk, so it is safe to run against
// multi-gigabyte bundles.
//
// In text mode the layout is section-based and rendered with
// text/tabwriter. In JSON mode the parsed manifest is re-emitted
// verbatim so callers can pipe into jq without parsing a bespoke
// inspect schema.
func Inspect(opts InspectOpts) error {
	if opts.Out == nil {
		return errors.New("InspectOpts.Out is required")
	}
	if opts.BundlePath == "" {
		return errors.New("InspectOpts.BundlePath is required")
	}

	manifestBytes, err := archive.ReadFileFromArchive(opts.BundlePath, "manifest.json")
	if err != nil {
		return fmt.Errorf("reading manifest.json from bundle: %w", err)
	}
	manifest, err := bundle.Parse(manifestBytes)
	if err != nil {
		return fmt.Errorf("parsing manifest.json: %w", err)
	}

	if opts.JSON {
		enc := json.NewEncoder(opts.Out)
		enc.SetIndent("", "  ")
		return enc.Encode(manifest)
	}

	archiveHash, err := hashFile(opts.BundlePath)
	if err != nil {
		return fmt.Errorf("hashing bundle: %w", err)
	}
	sidecarHash := readSidecarHash(opts.BundlePath)

	renderText(opts.Out, opts.BundlePath, manifest, archiveHash, sidecarHash)
	return nil
}

// hashFile streams f and returns the lowercase hex SHA256.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// readSidecarHash returns the hex hash recorded in
// <bundlePath>.sha256 (GNU coreutils format) or the empty string if
// the sidecar is missing or malformed.
func readSidecarHash(bundlePath string) string {
	raw, err := os.ReadFile(bundlePath + ".sha256")
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(raw))
	if line == "" {
		return ""
	}
	// "<hex>  <basename>" or "<hex> *<basename>"; take the first token.
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

const emptyValue = "—"

// display returns s or the em-dash placeholder when s is empty.
// Used for rendering optional manifest fields that should not show
// as blank gaps when absent.
func display(s string) string {
	if s == "" {
		return emptyValue
	}
	return s
}

// shortHash returns the first 12 hex characters of h, or the full
// string when shorter. Renders to "—" when empty.
func shortHash(h string) string {
	if h == "" {
		return emptyValue
	}
	if len(h) <= 12 {
		return h
	}
	return h[:12]
}

func renderText(w io.Writer, bundlePath string, m *bundle.Manifest, archiveHash, sidecarHash string) {
	fmt.Fprintf(w, "Bundle:   %s\n", bundlePath)
	fmt.Fprintf(w, "Version:  %s\n", display(m.BundleVersion))
	fmt.Fprintf(w, "Built:    %s by %s (%s, git %s)\n",
		display(m.BuildInfo.Timestamp),
		display(m.BuildInfo.Builder),
		display(m.BuildInfo.GoVersion),
		shortHash(m.BuildInfo.GitSHA))

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Integrity")
	fmt.Fprintf(w, "  Archive SHA256:   %s  %s\n", shortHash(archiveHash), sidecarStatus(archiveHash, sidecarHash))
	fmt.Fprintf(w, "  Manifest claim:   %s  %s\n", shortHash(m.BundleSHA256), manifestClaimStatus(archiveHash, m.BundleSHA256))

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Components")
	renderComponents(w, m)

	if m.Components.Onramp != nil && len(m.Components.Onramp.Patches) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Patches (onramp)")
		renderPatches(w, m.Components.Onramp.Patches)
	}
}

func sidecarStatus(archiveHash, sidecarHash string) string {
	switch sidecarHash {
	case "":
		return "(no sidecar found)"
	case archiveHash:
		return "(matches sidecar)"
	default:
		return fmt.Sprintf("(MISMATCH sidecar=%s)", shortHash(sidecarHash))
	}
}

func manifestClaimStatus(archiveHash, claimed string) string {
	switch claimed {
	case "":
		// patch-bundle clears BundleSHA256 deliberately. Call that
		// out so users don't read the empty slot as an error.
		return "(not recorded — likely patched after build)"
	case archiveHash:
		return "(matches)"
	default:
		return "(MISMATCH)"
	}
}

func renderComponents(w io.Writer, m *bundle.Manifest) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	defer tw.Flush()

	if rk := m.Components.RKE2; rk != nil {
		variants := "—"
		if len(rk.Variants) > 0 {
			variants = strings.Join(rk.Variants, ",")
		}
		fmt.Fprintf(tw, "  RKE2\t%s\tvariants: %s; image_mode: %s\n",
			display(rk.Version), variants, display(rk.ImageMode))
	}
	if h := m.Components.Helm; h != nil {
		fmt.Fprintf(tw, "  Helm\t%s\t\n", display(h.Version))
	}
	if ao := m.Components.AetherOps; ao != nil {
		fmt.Fprintf(tw, "  aether-ops\t%s\t%s\n", display(ao.Version), formatAetherOpsSource(ao.Source))
	}
	if o := m.Components.Onramp; o != nil {
		ref := display(o.Ref)
		fmt.Fprintf(tw, "  onramp\tref: %s\tsha: %s  tree: %s\n",
			ref, shortHash(o.ResolvedSHA), shortHash(o.TreeSHA256))
	}
	for _, hc := range m.Components.HelmCharts {
		fmt.Fprintf(tw, "  helm-charts/%s\tref: %s\tsha: %s  tree: %s\n",
			hc.Name, display(hc.Ref), shortHash(hc.ResolvedSHA), shortHash(hc.TreeSHA256))
	}
}

// formatAetherOpsSource renders the provenance one-liner shown on the
// aether-ops component line. Returns "source: —" when no Source has
// been recorded (older bundles).
func formatAetherOpsSource(s *bundle.AetherOpsSource) string {
	if s == nil {
		return "source: " + emptyValue
	}
	parts := []string{"source: " + display(s.Mode)}
	if s.Ref != "" {
		parts = append(parts, "ref: "+s.Ref)
	}
	if s.Repo != "" {
		parts = append(parts, "repo: "+s.Repo)
	}
	if s.ResolvedSHA != "" {
		parts = append(parts, "sha: "+shortHash(s.ResolvedSHA))
	}
	if s.FrontendRef != "" {
		parts = append(parts, "frontend: "+s.FrontendRef)
	}
	if s.LocalPath != "" {
		parts = append(parts, "path: "+s.LocalPath)
	}
	return strings.Join(parts, "  ")
}

func renderPatches(w io.Writer, patches []bundle.PatchRecord) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	defer tw.Flush()
	for _, p := range patches {
		kind := "[" + display(p.Kind) + "]"
		ts := display(p.Timestamp)
		target := display(p.Target)
		source := p.Source
		arrow := ""
		if source != "" && source != "<inline content>" {
			arrow = " ← " + source
		} else if source == "<inline content>" {
			arrow = " (inline)"
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s%s\n", kind, ts, target, arrow)
	}
}
