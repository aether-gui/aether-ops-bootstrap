package builder

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestExtractImagesFromChart_PlainString(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "values.yaml"), `
image: ghcr.io/example/app:v1
replicaCount: 3
`)

	got, err := ExtractImagesFromChart(dir)
	if err != nil {
		t.Fatalf("ExtractImagesFromChart: %v", err)
	}
	want := []string{"ghcr.io/example/app:v1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestExtractImagesFromChart_Descriptor(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "values.yaml"), `
image:
  registry: docker.io
  repository: grafana/grafana
  tag: "10.0.0"
  pullPolicy: IfNotPresent
`)

	got, err := ExtractImagesFromChart(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"docker.io/grafana/grafana:10.0.0"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestExtractImagesFromChart_DescriptorRepoAlreadyHasRegistry(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "values.yaml"), `
image:
  repository: ghcr.io/example/app
  tag: v2
`)
	got, err := ExtractImagesFromChart(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ghcr.io/example/app:v2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestExtractImagesFromChart_SDCorePrefixStyle(t *testing.T) {
	// Real SD-Core format: repository is a registry prefix with a
	// trailing slash, and each tag value is the image name plus version.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "values.yaml"), `
images:
  repository: "ghcr.io/omec-project/"
  pullPolicy: Always
  tags:
    amf: 5gc-amf:rel-2.1.1
    smf: 5gc-smf:rel-2.1.0
`)
	got, err := ExtractImagesFromChart(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"ghcr.io/omec-project/5gc-amf:rel-2.1.1",
		"ghcr.io/omec-project/5gc-smf:rel-2.1.0",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestExtractImagesFromChart_SDCoreMixedRegistries(t *testing.T) {
	// omec-control-plane has an empty repository prefix and the tag
	// values carry full image refs including distinct registries.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "values.yaml"), `
images:
  repository: ""
  tags:
    init: omecproject/busybox:stable
    depCheck: quay.io/stackanetes/kubernetes-entrypoint:v0.3.1
`)
	got, err := ExtractImagesFromChart(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"omecproject/busybox:stable",
		"quay.io/stackanetes/kubernetes-entrypoint:v0.3.1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestExtractImagesFromChart_NestedSubchart(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "charts", "amf", "values.yaml"), `
image: ghcr.io/omec-project/amf:rel-1.8.0
`)
	writeFile(t, filepath.Join(dir, "charts", "smf", "values.yaml"), `
image: ghcr.io/omec-project/smf:rel-1.8.0
`)

	got, err := ExtractImagesFromChart(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"ghcr.io/omec-project/amf:rel-1.8.0",
		"ghcr.io/omec-project/smf:rel-1.8.0",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestExtractImagesFromChart_Dedupes(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "values.yaml"), `
image: ghcr.io/example/app:v1
`)
	writeFile(t, filepath.Join(dir, "charts", "sub", "values.yaml"), `
image: ghcr.io/example/app:v1
`)

	got, err := ExtractImagesFromChart(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("got %v, want exactly one deduped entry", got)
	}
}

func TestExtractImagesFromChart_SkipsTemplateExpressions(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "values.yaml"), `
image: '{{ .Values.image.repository }}:{{ .Values.image.tag }}'
`)

	got, err := ExtractImagesFromChart(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("template expressions should be skipped, got %v", got)
	}
}

func TestExtractImagesFromChart_RejectsInvalid(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "values.yaml"), `
image: "not-a-valid-ref"
port: 8080
`)

	got, err := ExtractImagesFromChart(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("invalid ref should be skipped, got %v", got)
	}
}

func TestExtractImagesFromChart_DescriptorWithoutTagIsSkipped(t *testing.T) {
	// Rejecting untagged descriptors prevents accidental :latest pulls
	// in airgap bundles. The operator must pin a tag in the spec's
	// `images.extra` list if they really want the image.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "values.yaml"), `
image:
  repository: bitnamilegacy/kafka
`)
	got, err := ExtractImagesFromChart(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("descriptor without tag should be skipped, got %v", got)
	}
}

func TestExtractImagesFromChart_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	got, err := ExtractImagesFromChart(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("empty dir should produce no results, got %v", got)
	}
}
