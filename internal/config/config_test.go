package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cRolandoJr/ailoop/internal/state"

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

func TestProvidersConClaveDesconocidaNoCarga(t *testing.T) {
	// Un typo ignorado en silencio es el mismo modo de falla ya medido con
	// GEMINI_API_KEY ensombreciendo al local: el ruteo que creiste configurar
	// no existe y nada avisa. La clave mal escrita tiene que NOMBRARSE.
	dir := t.TempDir()
	if err := Save(dir, &Config{Providers: map[string]string{"verfication": "claude"}}); err != nil {
		t.Fatal(err)
	}
	_, err := Load(dir)
	if err == nil {
		t.Fatal("cargo un config con una fase inexistente en providers")
	}
	if !strings.Contains(err.Error(), "verfication") {
		t.Errorf("el error no nombra la clave rota: %v", err)
	}
}

func TestProvidersValidosCarganYSeMapeanAFases(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, &Config{Providers: map[string]string{
		"default":      "openai",
		"discovery":    "claude",
		"verification": "gemini",
	}}); err != nil {
		t.Fatal(err)
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	byPhase, def := c.PhaseProviders()
	if def != "openai" {
		t.Errorf("default = %q, quiero openai", def)
	}
	if byPhase[state.PhaseDiscovery] != "claude" || byPhase[state.PhaseVerification] != "gemini" {
		t.Errorf("mapa por fase = %v", byPhase)
	}
	if _, ok := byPhase[state.PhasePlan]; ok {
		t.Error("invento una entrada para una fase no listada")
	}
}

func TestSinProvidersNoHayRuteo(t *testing.T) {
	c := &Config{}
	byPhase, def := c.PhaseProviders()
	if len(byPhase) != 0 || def != "" {
		t.Errorf("un config sin providers ruteo algo: %v, %q", byPhase, def)
	}
}
