package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cRolandoJr/ailoop/internal/verify"
)

func TestLoadMissingConfigIsNotAnEmptyConfig(t *testing.T) {
	// Un config ausente NO puede devolver una lista vacia de checks: eso haria
	// que "no hay nada que verificar" pase por "todo verificado".
	_, err := Load(t.TempDir())
	if err != ErrNotFound {
		t.Errorf("err = %v, quiero ErrNotFound", err)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	original := &Config{
		TimeoutSeconds: 42,
		Verify: []verify.Check{
			{Name: "build", Cmd: "go build ./..."},
			{Name: "test", Cmd: "go test ./... -count=1"},
		},
	}

	if err := Save(dir, original); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got.TimeoutSeconds != original.TimeoutSeconds {
		t.Errorf("TimeoutSeconds = %d, quiero %d", got.TimeoutSeconds, original.TimeoutSeconds)
	}
	if len(got.Verify) != len(original.Verify) {
		t.Fatalf("Verify tiene %d checks, quiero %d", len(got.Verify), len(original.Verify))
	}
	for i := range original.Verify {
		if got.Verify[i] != original.Verify[i] {
			t.Errorf("Verify[%d] = %+v, quiero %+v", i, got.Verify[i], original.Verify[i])
		}
	}
}

func TestDetectFindsGoProject(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0644); err != nil {
		t.Fatal(err)
	}

	c := Detect(dir)
	if len(c.Verify) == 0 {
		t.Fatalf("Detect no encontro checks en un proyecto con go.mod")
	}
	var tieneTest bool
	for _, chk := range c.Verify {
		if chk.Name == "test" {
			tieneTest = true
		}
	}
	if !tieneTest {
		t.Errorf("Detect no incluyo el check de test: %+v", c.Verify)
	}
}

func TestDetectDoesNotGuessUnknownStack(t *testing.T) {
	// Sin evidencia del stack, la lista queda vacia y el usuario la llena.
	// Inventar un comando plausible seria exactamente la alucinacion que este
	// paquete existe para no tener.
	c := Detect(t.TempDir())
	if len(c.Verify) != 0 {
		t.Errorf("Detect invento %d checks para un directorio vacio: %+v", len(c.Verify), c.Verify)
	}
}

func TestTimeoutDefaultsWhenZero(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, &Config{TimeoutSeconds: 0}); err != nil {
		t.Fatal(err)
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.TimeoutSeconds != DefaultTimeoutSeconds {
		t.Errorf("TimeoutSeconds = %d, quiero %d", c.TimeoutSeconds, DefaultTimeoutSeconds)
	}
}
