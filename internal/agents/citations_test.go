package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cRolandoJr/ailoop/internal/state"
)

func TestCitacionAArchivoInexistenteSeDetecta(t *testing.T) {
	// Es el anti-alucinacion mas barato que hay: el agente afirma que algo
	// esta en un archivo, y el archivo no existe.
	malas := checkCitations(t.TempDir(), "Como se ve en [internal/noexiste.go:12], el flujo falla.")
	if len(malas) != 1 {
		t.Fatalf("detecto %d citas invalidas, quiero 1: %v", len(malas), malas)
	}
	if !strings.Contains(malas[0], "not found") {
		t.Errorf("el motivo no es claro: %q", malas[0])
	}
}

func TestCitacionAArchivoRealPasa(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "real.go"), []byte("package x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if malas := checkCitations(ws, "ver [real.go:1]"); len(malas) != 0 {
		t.Errorf("marco como invalida una cita correcta: %v", malas)
	}
}

func TestCitacionConRangoDeLineas(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "real.go"), []byte("a\nb\nc\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if malas := checkCitations(ws, "ver [real.go:1-3]"); len(malas) != 0 {
		t.Errorf("rechazo un rango valido: %v", malas)
	}
}

func TestCitacionQueEscapaDelWorkspaceSeRechaza(t *testing.T) {
	for _, cita := range []string{"[/etc/passwd:1]", "[../../secreto.txt:4]"} {
		malas := checkCitations(t.TempDir(), "mira "+cita)
		if len(malas) != 1 {
			t.Errorf("%s: detecto %d, quiero 1", cita, len(malas))
			continue
		}
		if !strings.Contains(malas[0], "unsafe") {
			t.Errorf("%s: motivo = %q, quiero que diga unsafe", cita, malas[0])
		}
	}
}

func TestUnDirectorioNoEsUnaCitaValida(t *testing.T) {
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, "internal"), 0755); err != nil {
		t.Fatal(err)
	}
	if malas := checkCitations(ws, "ver [internal:1]"); len(malas) != 1 {
		t.Errorf("acepto un directorio como cita: %v", malas)
	}
}

func TestTextoSinCitasNoProduceFalsosPositivos(t *testing.T) {
	// Un chequeo ruidoso se termina desactivando, asi que esto importa.
	textos := []string{
		"El servidor corre en http://localhost:8080 y anda bien.",
		"La relacion es 3:1 y el ratio [importante] no cambia.",
		"Sin citas de ningun tipo.",
	}
	for _, txt := range textos {
		if malas := checkCitations(t.TempDir(), txt); len(malas) != 0 {
			t.Errorf("falso positivo en %q: %v", txt, malas)
		}
	}
}

func TestExtractDecisionsRegistraElFormatoDelRepo(t *testing.T) {
	s := state.NewState("tarea")
	extractDecisions(s, "Analisis.\n<<<< DECISION\nStatement: el config va en JSON\nRationale: cero dependencias\n>>>>\nSigo.")

	if len(s.Record.Decisions) != 1 {
		t.Fatalf("registro %d decisiones, quiero 1", len(s.Record.Decisions))
	}
	if s.Record.Decisions[0].Statement != "el config va en JSON" {
		t.Errorf("Statement = %q", s.Record.Decisions[0].Statement)
	}
	if s.Record.Decisions[0].Rationale != "cero dependencias" {
		t.Errorf("Rationale = %q", s.Record.Decisions[0].Rationale)
	}
}

func TestExtractDecisionsIgnoraProsaSuelta(t *testing.T) {
	s := state.NewState("tarea")
	extractDecisions(s, "Creo que convendria usar JSON en vez de YAML.")
	if len(s.Record.Decisions) != 0 {
		t.Errorf("infirio una decision de prosa: %+v", s.Record.Decisions)
	}
}

func TestExtractDecisionsIgnoraBloqueSinCerrar(t *testing.T) {
	s := state.NewState("tarea")
	extractDecisions(s, "<<<< DECISION\nStatement: algo\n")
	if len(s.Record.Decisions) != 0 {
		t.Errorf("acepto un bloque sin cerrar: %+v", s.Record.Decisions)
	}
}
