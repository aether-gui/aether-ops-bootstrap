// Package builder's aptrepo helpers turn a staged set of bundled .deb
// files into a self-contained file:// apt repository the launcher can
// hand to apt-get install. The repo follows the standard Ubuntu archive
// layout:
//
//	apt-repo/
//	  dists/<codename>/
//	    Release
//	    main/binary-<arch>/
//	      Packages
//	      Packages.gz
//	  pool/<codename>/<arch>/<basename>.deb
//
// Per-arch Packages contains the upstream RFC 822 control stanza for
// each bundled deb (preserved by the parser as Package.RawStanza), with
// the Filename: line rewritten to point at the local pool/ layout.
// SHA256/Size are reused from the upstream Packages.gz we already
// fetched, so no per-deb re-hashing is required. Release is generated
// fresh and lists the per-component Packages files with MD5/SHA1/SHA256
// computed against the on-disk content the builder just wrote.
//
// v1 ships unsigned; the consumer drops a sources.list with
// `[trusted=yes]`. GPG signing is a follow-up.
package builder

import (
	"bytes"
	"compress/gzip"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aether-gui/aether-ops-bootstrap/internal/deb"
)

// AptRepoComponent is the only component the builder emits. Multiple
// components (universe, restricted, …) are out of scope: every package
// the bundle needs lives in one synthetic "main".
const AptRepoComponent = "main"

// AptRepoOrigin and AptRepoLabel identify the bundle in apt's output
// (e.g. "Origin aether-ops" appears in `apt-cache policy`). Cosmetic
// but operator-visible.
const (
	AptRepoOrigin = "aether-ops"
	AptRepoLabel  = "aether-ops bundle"
)

// BuildAptRepo writes Packages/Packages.gz and Release files under
// stageDir/apt-repo/dists/<codename>/ for every (codename, arch)
// combination present in pkgs. Each package's bytes are pulled from
// pkg.RawStanza; the stanza's Filename: line is rewritten to point at
// pool/<codename>/<arch>/<basename>.deb so apt resolves URLs relative
// to the repo root.
//
// Callers are responsible for staging the actual .deb files at
// stageDir/apt-repo/pool/<codename>/<arch>/<basename>.deb before this
// runs — BuildAptRepo only writes the metadata.
func BuildAptRepo(stageDir string, pkgs []*deb.Package, codenames []string) error {
	if len(pkgs) == 0 || len(codenames) == 0 {
		return nil
	}

	repoRoot := filepath.Join(stageDir, "apt-repo")

	// Group packages by (codename, arch). The codename comes from the
	// caller, not the package — multiple pockets (noble + noble-updates)
	// roll up into a single base codename in the repo we emit.
	type key struct{ codename, arch string }
	groups := make(map[key][]*deb.Package)
	for _, p := range pkgs {
		// Architecture "all" lands under binary-all; every other arch
		// lands under its own binary-<arch>.
		groups[key{codename: codenames[0], arch: p.Arch}] = append(
			groups[key{codename: codenames[0], arch: p.Arch}], p,
		)
	}
	// Multi-codename support is out of scope for v1 but the layout
	// already extends cleanly: one Release + dists/<codename>/* per
	// codename, sharing the pool/ tree.
	if len(codenames) > 1 {
		return fmt.Errorf("apt-repo builder does not support multi-codename bundles yet (got %v)", codenames)
	}
	codename := codenames[0]

	// Determine the set of architectures we ended up with so Release
	// can declare them.
	archSet := make(map[string]bool)
	for k := range groups {
		archSet[k.arch] = true
	}
	arches := make([]string, 0, len(archSet))
	for a := range archSet {
		arches = append(arches, a)
	}
	sort.Strings(arches)

	// Per-arch Packages files.
	for _, arch := range arches {
		archDir := filepath.Join(repoRoot, "dists", codename, AptRepoComponent, "binary-"+arch)
		if err := os.MkdirAll(archDir, 0755); err != nil {
			return fmt.Errorf("creating %s: %w", archDir, err)
		}
		group := groups[key{codename: codename, arch: arch}]
		// Stable order so successive builds produce byte-identical Packages
		// (same name → same version pinned via lockfile).
		sort.Slice(group, func(i, j int) bool {
			if group[i].Name != group[j].Name {
				return group[i].Name < group[j].Name
			}
			return group[i].Version < group[j].Version
		})

		var plain bytes.Buffer
		for i, p := range group {
			if len(p.RawStanza) == 0 {
				return fmt.Errorf("package %s/%s has no RawStanza; cannot emit Packages entry",
					p.Name, p.Version)
			}
			rewritten := rewriteFilenameLine(p.RawStanza, codename, arch)
			plain.Write(rewritten)
			// Stanzas in a Packages file are separated by exactly one
			// blank line. Avoid a trailing blank line at the very end.
			if i < len(group)-1 {
				plain.WriteByte('\n')
			}
		}

		packagesPath := filepath.Join(archDir, "Packages")
		if err := os.WriteFile(packagesPath, plain.Bytes(), 0644); err != nil {
			return fmt.Errorf("writing %s: %w", packagesPath, err)
		}

		gzPath := packagesPath + ".gz"
		if err := writeGzip(gzPath, plain.Bytes()); err != nil {
			return fmt.Errorf("writing %s: %w", gzPath, err)
		}
	}

	// Release per codename.
	if err := writeRelease(repoRoot, codename, arches); err != nil {
		return err
	}

	return nil
}

// rewriteFilenameLine returns a copy of stanza with its `Filename:`
// line rewritten to point at the local pool layout. The original line
// from the upstream Packages index points at the upstream mirror's
// pool/, which is irrelevant once we re-host. Other lines are passed
// through verbatim.
func rewriteFilenameLine(stanza []byte, codename, arch string) []byte {
	const prefix = "Filename:"
	lines := bytes.Split(stanza, []byte("\n"))
	for i, line := range lines {
		if !bytes.HasPrefix(line, []byte(prefix)) {
			continue
		}
		// Original: Filename: pool/main/g/git/git_2.43.0-1ubuntu7_amd64.deb
		// Rewrite to: Filename: pool/<codename>/<arch>/<basename>.deb
		old := strings.TrimSpace(string(line[len(prefix):]))
		basename := filepath.Base(old)
		lines[i] = fmt.Appendf(nil, "Filename: pool/%s/%s/%s", codename, arch, basename)
		break
	}
	return bytes.Join(lines, []byte("\n"))
}

// writeRelease emits dists/<codename>/Release with the Origin/Label/
// Suite/Codename/Components/Architectures/Date headers plus MD5Sum,
// SHA1, and SHA256 blocks listing each component-arch's Packages and
// Packages.gz with size + hash. Hashes are computed fresh against the
// files this builder just wrote.
func writeRelease(repoRoot, codename string, arches []string) error {
	distDir := filepath.Join(repoRoot, "dists", codename)

	type fileEntry struct {
		path   string // disk path
		rel    string // path relative to dists/<codename>/, used in Release
		size   int64
		md5    string
		sha1   string
		sha256 string
	}
	var entries []fileEntry
	for _, arch := range arches {
		for _, name := range []string{"Packages", "Packages.gz"} {
			rel := filepath.ToSlash(filepath.Join(AptRepoComponent, "binary-"+arch, name))
			abs := filepath.Join(distDir, rel)
			info, err := os.Stat(abs)
			if err != nil {
				return fmt.Errorf("stat %s: %w", abs, err)
			}
			md5sum, sha1sum, sha256sum, err := hashFile(abs)
			if err != nil {
				return err
			}
			entries = append(entries, fileEntry{
				path:   abs,
				rel:    rel,
				size:   info.Size(),
				md5:    md5sum,
				sha1:   sha1sum,
				sha256: sha256sum,
			})
		}
	}

	var b bytes.Buffer
	fmt.Fprintf(&b, "Origin: %s\n", AptRepoOrigin)
	fmt.Fprintf(&b, "Label: %s\n", AptRepoLabel)
	fmt.Fprintf(&b, "Suite: %s\n", codename)
	fmt.Fprintf(&b, "Codename: %s\n", codename)
	fmt.Fprintf(&b, "Components: %s\n", AptRepoComponent)
	fmt.Fprintf(&b, "Architectures: %s\n", strings.Join(arches, " "))
	fmt.Fprintf(&b, "Date: %s\n", time.Now().UTC().Format(time.RFC1123))

	writeBlock := func(label string, get func(fileEntry) string) {
		fmt.Fprintf(&b, "%s:\n", label)
		for _, e := range entries {
			fmt.Fprintf(&b, " %s %d %s\n", get(e), e.size, e.rel)
		}
	}
	writeBlock("MD5Sum", func(e fileEntry) string { return e.md5 })
	writeBlock("SHA1", func(e fileEntry) string { return e.sha1 })
	writeBlock("SHA256", func(e fileEntry) string { return e.sha256 })

	releasePath := filepath.Join(distDir, "Release")
	if err := os.WriteFile(releasePath, b.Bytes(), 0644); err != nil {
		return fmt.Errorf("writing %s: %w", releasePath, err)
	}
	return nil
}

func writeGzip(path string, data []byte) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	if _, err := gz.Write(data); err != nil {
		gz.Close()
		return err
	}
	return gz.Close()
}

func hashFile(path string) (md5sum, sha1sum, sha256sum string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", "", err
	}
	defer f.Close()

	hMD5 := md5.New()
	hSHA1 := sha1.New()
	hSHA256 := sha256.New()
	w := io.MultiWriter(hMD5, hSHA1, hSHA256)
	if _, err := io.Copy(w, f); err != nil {
		return "", "", "", err
	}
	return hex.EncodeToString(sumOf(hMD5)),
		hex.EncodeToString(sumOf(hSHA1)),
		hex.EncodeToString(sumOf(hSHA256)),
		nil
}

func sumOf(h hash.Hash) []byte { return h.Sum(nil) }
