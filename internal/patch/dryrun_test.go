package patch

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func escribir(t *testing.T, ws, rel, contenido string) {
	t.Helper()
	p := filepath.Join(ws, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(contenido), 0o644); err != nil {
		t.Fatal(err)
	}
}

// El modo de falla numero uno de un agente de SEARCH/REPLACE es citar un
// contexto viejo. Hoy eso se descubre al aplicar, o sea despues de que la
// persona ya decidio.
func TestDryRunSenalaElBloqueQueNoMatchea(t *testing.T) {
	ws := t.TempDir()
	escribir(t, ws, "a.go", "package a\n\nfunc Uno() {}\n")

	bloques := []Block{
		{FilePath: "a.go", Search: "func Uno() {}", Replace: "func Uno() { println(1) }"},
		{FilePath: "a.go", Search: "func Dos() {}", Replace: "func Dos() { println(2) }"},
	}

	problemas := DryRun(ws, bloques, NewSeen("a.go"))

	if len(problemas) != 1 {
		t.Fatalf("hay %d problemas, quiero 1: %v", len(problemas), problemas)
	}
	if problemas[0].Index != 1 {
		t.Errorf("el problema apunta al bloque %d, quiero el 1", problemas[0].Index)
	}

	// Seco quiere decir seco.
	data, _ := os.ReadFile(filepath.Join(ws, "a.go"))
	if string(data) != "package a\n\nfunc Uno() {}\n" {
		t.Errorf("DryRun escribio en el disco: %q", string(data))
	}
}

func TestDryRunSenalaElPathQueEscapa(t *testing.T) {
	ws := t.TempDir()
	bloques := []Block{{FilePath: "../../etc/passwd", Search: "x", Replace: "y"}}

	problemas := DryRun(ws, bloques, nil)

	if len(problemas) != 1 {
		t.Fatalf("hay %d problemas, quiero 1", len(problemas))
	}
	if !errors.Is(problemas[0].Err, ErrOutsideWorkspace) {
		t.Errorf("Err = %v, quiero que envuelva ErrOutsideWorkspace", problemas[0].Err)
	}
	// El Problem ya trae File e Index: repetirlos dentro del error hace que
	// la linea que lee la persona nombre el archivo dos veces.
	if strings.Contains(problemas[0].Err.Error(), "refusing patch") {
		t.Errorf("Err = %v, quiero la causa cruda, no el envoltorio de planWrites", problemas[0].Err)
	}
}

func TestDryRunSenalaElArchivoQueElAgenteNoLeyo(t *testing.T) {
	ws := t.TempDir()
	escribir(t, ws, "b.go", "package b\n")

	bloques := []Block{{FilePath: "b.go", Search: "package b", Replace: "package bb"}}

	problemas := DryRun(ws, bloques, NewSeen()) // leyo nada
	if len(problemas) != 1 {
		t.Fatalf("hay %d problemas, quiero 1", len(problemas))
	}
}

func TestDryRunCallaCuandoTodoAplica(t *testing.T) {
	ws := t.TempDir()
	escribir(t, ws, "c.go", "package c\n\nfunc Uno() {}\n")

	bloques := []Block{
		{FilePath: "c.go", Search: "func Uno() {}", Replace: "func Uno() { println(1) }"},
		{FilePath: "nuevo.go", Search: "", Replace: "package nuevo\n"},
	}

	if problemas := DryRun(ws, bloques, NewSeen("c.go")); len(problemas) != 0 {
		t.Fatalf("quiero silencio, hay %v", problemas)
	}
	if _, err := os.Stat(filepath.Join(ws, "nuevo.go")); err == nil {
		t.Error("DryRun creo el archivo nuevo")
	}
}

// Los mismos bloques siguen viaje a la revision y despues a ApplyBlocks.
// planWrites reescribe FilePath sobre el slice del llamador; el ensayo no
// puede dejar rastro en lo que se va a revisar.
func TestDryRunNoMutaLosBloques(t *testing.T) {
	ws := t.TempDir()
	escribir(t, ws, "sub/d.go", "package d\n")

	bloques := []Block{{FilePath: "./sub/../sub/d.go", Search: "package d", Replace: "package dd"}}
	original := bloques[0].FilePath

	DryRun(ws, bloques, NewSeen("sub/d.go"))

	if bloques[0].FilePath != original {
		t.Errorf("DryRun dejo FilePath en %q, era %q", bloques[0].FilePath, original)
	}
}
