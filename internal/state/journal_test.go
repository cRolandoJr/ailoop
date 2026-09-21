package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cRolandoJr/ailoop/internal/llm"
)

func TestElJournalGuardaUnaLineaPorTurnoYLaDevuelve(t *testing.T) {
	ws := t.TempDir()

	AppendTurn(ws, Turn{
		Phase:       "discovery",
		Model:       "claude-opus-5",
		Rounds:      3,
		Usage:       llm.Usage{InputTokens: 100, OutputTokens: 20},
		FilesRead:   []string{"internal/state/spend.go"},
		CommandsRun: []string{"go test ./internal/state"},
	})
	AppendTurn(ws, Turn{Phase: "design", Rounds: 1})

	turns, err := ReadTurns(ws, time.Time{})
	if err != nil {
		t.Fatalf("ReadTurns: %v", err)
	}
	if len(turns) != 2 {
		t.Fatalf("esperaba 2 turnos, hay %d", len(turns))
	}
	if turns[0].Phase != "discovery" || turns[1].Phase != "design" {
		t.Errorf("el orden de escritura no se conserva: %q, %q", turns[0].Phase, turns[1].Phase)
	}
	if got := turns[0].FilesRead; len(got) != 1 || got[0] != "internal/state/spend.go" {
		t.Errorf("files_read no sobrevivió el viaje: %v", got)
	}
	if got := turns[0].CommandsRun; len(got) != 1 {
		t.Errorf("commands_run no sobrevivió el viaje: %v", got)
	}
	if turns[0].Model != "claude-opus-5" {
		t.Errorf("sin el modelo no se puede distinguir una regresión de pesos de una de prompt; quedó %q", turns[0].Model)
	}
}

// El journal lo escribe un proceso que se puede matar a mitad de línea, así que
// una última línea truncada es un estado NORMAL. Si eso invalidara la lectura,
// el log sería inútil justo cuando algo salió mal.
func TestUnaLineaCorruptaNoInvalidaElRestoDelJournal(t *testing.T) {
	ws := t.TempDir()
	AppendTurn(ws, Turn{Phase: "discovery"})
	AppendTurn(ws, Turn{Phase: "design"})

	f, err := os.OpenFile(JournalPath(ws), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("abriendo el journal: %v", err)
	}
	if _, err := f.WriteString(`{"phase":"implementa`); err != nil {
		t.Fatalf("escribiendo la línea truncada: %v", err)
	}
	f.Close()

	turns, err := ReadTurns(ws, time.Time{})
	if err != nil {
		t.Fatalf("una línea truncada no debe fallar la lectura: %v", err)
	}
	if len(turns) != 2 {
		t.Fatalf("esperaba los 2 turnos sanos, hay %d", len(turns))
	}
}

func TestElCutoffDejaAfueraLoViejo(t *testing.T) {
	ws := t.TempDir()
	ayer := time.Now().Add(-24 * time.Hour)
	AppendTurn(ws, Turn{Phase: "vieja", Time: ayer})
	AppendTurn(ws, Turn{Phase: "nueva"})

	turns, err := ReadTurns(ws, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("ReadTurns: %v", err)
	}
	if len(turns) != 1 || turns[0].Phase != "nueva" {
		t.Fatalf("el cutoff no filtró: %+v", turns)
	}
}

// AppendTurn no devuelve error a propósito: registrar no puede ser el motivo de
// que una fase falle. Este test fija esa conducta — si algún día empieza a
// paniquear, el costo no es un hueco en el log sino el trabajo entero.
func TestRegistrarNuncaRompeAunqueNoSePuedaEscribir(t *testing.T) {
	ws := t.TempDir()
	// Un archivo donde debería ir el directorio .ailoop: el MkdirAll falla.
	if err := os.WriteFile(filepath.Join(ws, ".ailoop"), []byte("no soy un directorio"), 0o644); err != nil {
		t.Fatalf("preparando el estorbo: %v", err)
	}

	AppendTurn(ws, Turn{Phase: "discovery"})

	if _, err := ReadTurns(ws, time.Time{}); err == nil {
		t.Log("la lectura tampoco rompió")
	}
}

func TestSinJournalLaLecturaDevuelveVacioYNoError(t *testing.T) {
	turns, err := ReadTurns(t.TempDir(), time.Time{})
	if err != nil {
		t.Fatalf("un workspace sin turnos no es un error: %v", err)
	}
	if len(turns) != 0 {
		t.Fatalf("esperaba vacío, hay %d", len(turns))
	}
}
